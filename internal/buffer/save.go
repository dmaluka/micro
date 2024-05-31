package buffer

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"unicode"

	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/micro/v2/internal/screen"
	"github.com/zyedidia/micro/v2/internal/util"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// LargeFileThreshold is the number of bytes when fastdirty is forced
// because hashing is too slow
const LargeFileThreshold = 50000

// overwriteFile opens the given file for writing, truncating if one exists, and then calls
// the supplied function with the file as io.Writer object, also making sure the file is
// closed afterwards.
func overwriteFile(name string, enc encoding.Encoding, fn func(io.Writer) error, withSudo bool) (err error) {
	var writeCloser io.WriteCloser
	var screenb bool
	var cmd *exec.Cmd
	var c chan os.Signal

	if withSudo {
		cmd = exec.Command(config.GlobalSettings["sucmd"].(string), "dd", "bs=4k", "of="+name)

		if writeCloser, err = cmd.StdinPipe(); err != nil {
			return
		}

		c = make(chan os.Signal, 1)
		signal.Reset(os.Interrupt)
		signal.Notify(c, os.Interrupt)

		screenb = screen.TempFini()
		// need to start the process now, otherwise when we flush the file
		// contents to its stdin it might hang because the kernel's pipe size
		// is too small to handle the full file contents all at once
		if err = cmd.Start(); err != nil {
			screen.TempStart(screenb)

			signal.Notify(util.Sigterm, os.Interrupt)
			signal.Stop(c)

			return
		}
	} else if writeCloser, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, util.FileMode); err != nil {
		return
	}

	w := bufio.NewWriter(transform.NewWriter(writeCloser, enc.NewEncoder()))
	err = fn(w)

	if err2 := w.Flush(); err2 != nil && err == nil {
		err = err2
	}
	// Call Sync() on the file to make sure the content is safely on disk.
	// Does not work with sudo as we don't have direct access to the file.
	if !withSudo {
		f := writeCloser.(*os.File)
		if err2 := f.Sync(); err2 != nil && err == nil {
			err = err2
		}
	}
	if err2 := writeCloser.Close(); err2 != nil && err == nil {
		err = err2
	}

	if withSudo {
		// wait for dd to finish and restart the screen if we used sudo
		err := cmd.Wait()
		screen.TempStart(screenb)

		signal.Notify(util.Sigterm, os.Interrupt)
		signal.Stop(c)

		if err != nil {
			return err
		}
	}

	return
}

func (b *Buffer) overwrite(name string, withSudo bool) (int, error) {
	enc, err := htmlindex.Get(b.Settings["encoding"].(string))
	if err != nil {
		return 0, err
	}

	var size int
	fwriter := func(file io.Writer) error {
		b.Lock()
		defer b.Unlock()

		if len(b.lines) == 0 {
			return err
		}

		// end of line
		var eol []byte
		if b.Endings == FFDos {
			eol = []byte{'\r', '\n'}
		} else {
			eol = []byte{'\n'}
		}

		// write lines
		if size, err = file.Write(b.lines[0].data); err != nil {
			return err
		}

		for _, l := range b.lines[1:] {
			if _, err = file.Write(eol); err != nil {
				return err
			}
			if _, err = file.Write(l.data); err != nil {
				return err
			}
			size += len(eol) + len(l.data)
		}
		return err
	}

	if err = overwriteFile(name, enc, fwriter, withSudo); err != nil {
		return size, err
	}

	return size, err
}

// Save saves the buffer to its default path
func (b *Buffer) Save() error {
	return b.SaveAs(b.Path)
}

// AutoSave saves the buffer to its default path
func (b *Buffer) AutoSave() error {
	// Doing full b.Modified() check every time would be costly, due to the hash
	// calculation. So use just isModified even if fastdirty is not set.
	if !b.isModified {
		return nil
	}
	return b.saveToFile(b.Path, false, true)
}

// SaveAs saves the buffer to a specified path (filename), creating the file if it does not exist
func (b *Buffer) SaveAs(filename string) error {
	return b.saveToFile(filename, false, false)
}

func (b *Buffer) SaveWithSudo() error {
	return b.SaveAsWithSudo(b.Path)
}

func (b *Buffer) SaveAsWithSudo(filename string) error {
	return b.saveToFile(filename, true, false)
}

func (b *Buffer) saveToFile(filename string, withSudo bool, autoSave bool) error {
	var err error
	if b.Type.Readonly {
		return errors.New("Cannot save readonly buffer")
	}
	if b.Type.Scratch {
		return errors.New("Cannot save scratch buffer")
	}
	if withSudo && runtime.GOOS == "windows" {
		return errors.New("Save with sudo not supported on Windows")
	}

	if !autoSave && b.Settings["rmtrailingws"].(bool) {
		for i, l := range b.lines {
			leftover := util.CharacterCount(bytes.TrimRightFunc(l.data, unicode.IsSpace))

			linelen := util.CharacterCount(l.data)
			b.Remove(Loc{leftover, i}, Loc{linelen, i})
		}

		b.RelocateCursors()
	}

	if b.Settings["eofnewline"].(bool) {
		end := b.End()
		if b.RuneAt(Loc{end.X - 1, end.Y}) != '\n' {
			b.insert(end, []byte{'\n'})
		}
	}

	// Update the last time this file was updated after saving
	defer func() {
		b.ModTime, _ = util.GetModTime(filename)
		err = b.Serialize()
	}()

	filename, err = util.ReplaceHome(filename)
	if err != nil {
		return err
	}

	newFile := false
	fileInfo, err := os.Stat(filename)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		newFile = true
	}
	if err == nil && fileInfo.IsDir() {
		return errors.New("Error: " + filename + " is a directory and cannot be saved")
	}
	if err == nil && !fileInfo.Mode().IsRegular() {
		return errors.New("Error: " + filename + " is not a regular file and cannot be saved")
	}

	absFilename, err := filepath.Abs(filename)
	if err != nil {
		return err
	}

	// Get the leading path to the file | "." is returned if there's no leading path provided
	if dirname := filepath.Dir(absFilename); dirname != "." {
		// Check if the parent dirs don't exist
		if _, statErr := os.Stat(dirname); errors.Is(statErr, fs.ErrNotExist) {
			// Prompt to make sure they want to create the dirs that are missing
			if b.Settings["mkparents"].(bool) {
				// Create all leading dir(s) since they don't exist
				if mkdirallErr := os.MkdirAll(dirname, os.ModePerm); mkdirallErr != nil {
					// If there was an error creating the dirs
					return mkdirallErr
				}
			} else {
				return errors.New("Parent dirs don't exist, enable 'mkparents' for auto creation")
			}
		}
	}

	size, err := b.safeWrite(absFilename, withSudo, newFile)
	if err != nil {
		return err
	}

	if !b.Settings["fastdirty"].(bool) {
		if size > LargeFileThreshold {
			// For large files 'fastdirty' needs to be on
			b.Settings["fastdirty"] = true
		} else {
			calcHash(b, &b.origHash)
		}
	}

	b.Path = filename
	b.AbsPath = absFilename
	b.isModified = false
	b.ReloadSettings(true)
	return err
}

// safeWrite writes the buffer to a file in a "safe" way, preventing loss of the
// contents of the file if it fails to write the new contents.
// This means that the file is not overwritten directly but by writing to the
// backup file first.
func (b *Buffer) safeWrite(path string, withSudo bool, newFile bool) (int, error) {
	backupDir := b.backupDir()
	if _, err := os.Stat(backupDir); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		if err = os.Mkdir(backupDir, os.ModePerm); err != nil {
			return 0, err
		}
	}

	backupName := util.DetermineEscapePath(backupDir, path)
	_, err := b.overwrite(backupName, false)
	if err != nil {
		os.Remove(backupName)
		return 0, err
	}

	b.forceKeepBackup = true
	size, err := b.overwrite(path, withSudo)
	if err != nil {
		if newFile {
			os.Remove(path)
		}
		return size, err
	}
	b.forceKeepBackup = false

	if !b.keepBackup() {
		os.Remove(backupName)
	}

	return size, err
}

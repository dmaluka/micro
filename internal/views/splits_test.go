package views

import (
	"fmt"
	"testing"
)

func TestHSplit(t *testing.T) {
	root := NewRoot(0, 0, 80, 80)
	n1 := root.VSplit(true)
	root.GetNode(n1).VSplit(true)
	root.GetNode(root.id).children[0].ResizeSplit(7)
	root.Resize(120, 120)

	fmt.Println(root.String())
}

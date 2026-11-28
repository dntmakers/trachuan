package pdftext

type Block struct {
	XMin, YMin, XMax, YMax string
	Lines                  []Line
}

type Line struct {
	XMin, YMin, XMax, YMax string
	Words                  []Word
}

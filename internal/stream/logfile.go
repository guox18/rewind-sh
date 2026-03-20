package stream

import (
	"bufio"
	"encoding/json"
	"os"
)

type LogWriter struct {
	f  *os.File
	bw *bufio.Writer
}

func NewLogWriter(path string) (*LogWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &LogWriter{
		f:  f,
		bw: bufio.NewWriterSize(f, 64*1024),
	}, nil
}

func (w *LogWriter) WriteEvent(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = w.bw.Write(b); err != nil {
		return err
	}
	if err = w.bw.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func (w *LogWriter) Close() error {
	if err := w.bw.Flush(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}

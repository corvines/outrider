package process

import (
	"os"
	"sync"
)

const serverLogMaxBytes int64 = 8 * 1024 * 1024

type rotatingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func openRotatingLog(path string, maxBytes int64) (*rotatingLog, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &rotatingLog{path: path, maxBytes: maxBytes, file: file, size: info.Size()}, nil
}

func (log *rotatingLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if int64(len(data)) > log.maxBytes {
		data = data[len(data)-int(log.maxBytes):]
	}
	if log.size+int64(len(data)) > log.maxBytes {
		if err := log.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := log.file.Write(data)
	log.size += int64(written)
	return written, err
}

func (log *rotatingLog) Close() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return nil
	}
	err := log.file.Close()
	log.file = nil
	return err
}

func (log *rotatingLog) rotate() error {
	if err := log.file.Close(); err != nil {
		return err
	}
	backup := log.path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(log.path, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(log.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	log.file = file
	log.size = 0
	return nil
}

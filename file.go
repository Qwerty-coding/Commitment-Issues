package main

import (
	"os"
	"path/filepath")

func writeToFiles(filename string, content []byte) error {
	path := filepath.Join("testfiles", filename)
	return os.WriteFile(path, content, 0644)
}
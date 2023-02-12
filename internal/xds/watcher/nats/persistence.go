package nats

import (
	"compress/gzip"
	"encoding/gob"
	"os"
	"path/filepath"
)

// dump whole data as gob
func dumpGob(dataPath string, name string, data interface{}) error {

	absPath, _ := filepath.Abs(dataPath + "/" + name)
	fi, err := os.Create(absPath)
	if err != nil {
		return err
	}
	defer fi.Close()

	fz := gzip.NewWriter(fi)
	defer fz.Close()

	encoder := gob.NewEncoder(fz)
	err = encoder.Encode(data)
	if err != nil {
		return err
	}

	return nil
}

// restore whole data from gob
func restoreGob(dataPath string, name string, data interface{}) error {

	absPath, _ := filepath.Abs(dataPath + "/" + name)
	fi, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer fi.Close()

	fz, err := gzip.NewReader(fi)
	if err != nil {
		return err
	}
	defer fz.Close()

	decoder := gob.NewDecoder(fz)
	err = decoder.Decode(data)
	if err != nil {
		return err
	}

	return nil
}

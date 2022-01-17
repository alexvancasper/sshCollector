package main

import (
  "path/filepath"
  "archive/zip"
  "io"
  "os"
  "time"
  "fmt"
)


func ZipFiles(filename string, files []string) error {
    newZipFile, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer newZipFile.Close()
    zipWriter := zip.NewWriter(newZipFile)
    defer zipWriter.Close()
    for _, file := range files {
        if err = AddFileToZip(zipWriter, file); err != nil {
            return err
        }
    }
    return nil
}

func AddFileToZip(zipWriter *zip.Writer, filename string) error {

    fileToZip, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer fileToZip.Close()

    // Get the file information
    info, err := fileToZip.Stat()
    if err != nil {
        return err
    }

    header, err := zip.FileInfoHeader(info)
    if err != nil {
        return err
    }

    // Using FileInfoHeader() above only uses the basename of the file. If we want
    // to preserve the folder structure we can overwrite this with the full path.
    header.Name = filename

    // Change to deflate to gain better compression
    // see http://golang.org/pkg/archive/zip/#pkg-constants
    header.Method = zip.Deflate

    writer, err := zipWriter.CreateHeader(header)
    if err != nil {
        return err
    }
    _, err = io.Copy(writer, fileToZip)
    return err
}

func zipping(files []string) (string, error) {
	t:=time.Now()
	output := fmt.Sprintf("%s%d%02d%02d_%02d%02d%02d.zip", conf.Common.Base_dir, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	if err := ZipFiles(output, files); err != nil {
		handleError(err)
	}
	if conf.Common.Remove {
		for _, name := range files {
			removename, error := filepath.Abs(name)
			handleError(error)
			err := os.Remove(removename)
			handleError(err)
		}
	}
	return filepath.Abs(output)
}

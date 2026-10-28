// Command gamedb turns MAME's XML list into the arcade plugin's game list:
//
//	gamedb mame0289lx.zip games.json.gz
//
// The input is the XML list (mame -listxml) or the zip mamedev publishes it
// in with each release.
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/yeixio/matinee/plugins/arcade/gamedb"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gamedb <mame -listxml output, or its zip> <games.json.gz>")
		os.Exit(2)
	}
	in, err := open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()
	db, err := gamedb.FromXML(in)
	if err != nil {
		log.Fatal(err)
	}
	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Write(out); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d games from MAME %s\n", len(db.Games), db.MAME)
}

func open(path string) (io.ReadCloser, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		return os.Open(path)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".xml") {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			return struct {
				io.Reader
				io.Closer
			}{r, zr}, nil
		}
	}
	zr.Close()
	return nil, fmt.Errorf("%s has no XML file in it", path)
}

package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Dup4/cf-tool/util"
	"github.com/fatih/color"
)

func less(a, b string) bool {
	reg := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	x := reg.FindSubmatch([]byte(a))
	y := reg.FindSubmatch([]byte(b))

	num := func(s []byte) int {
		n, _ := strconv.Atoi(string(s))
		return n
	}

	for i := 1; i <= 3; i++ {
		if num(x[i]) < num(y[i]) {
			return true
		} else if num(x[i]) > num(y[i]) {
			return false
		}
	}

	return false
}

func getLatest() (version, note, ptime, url string, size uint, err error) {
	goos := ""
	switch runtime.GOOS {
	case "darwin":
		goos = "darwin"
	case "linux":
		goos = "linux"
	case "windows":
		goos = "windows"
	default:
		err = fmt.Errorf("not support %v", runtime.GOOS)
		return
	}

	arch := ""
	switch runtime.GOARCH {
	case "386":
		arch = "386"
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		err = fmt.Errorf("not support %v", runtime.GOARCH)
		return
	}

	resp, err := http.Get("https://api.github.com/repos/Dup4/cf-tool/releases/latest")
	if err != nil {
		return
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	result := make(map[string]interface{})
	json.Unmarshal(body, &result)

	version = result["tag_name"].(string)
	note = result["body"].(string)
	tm, _ := time.Parse("2006-01-02T15:04:05Z", result["published_at"].(string))
	ptime = tm.In(time.Local).Format("2006-01-02 15:04")
	url = fmt.Sprintf("https://github.com/Dup4/cf-tool/releases/download/%v/cf-tool_%v_%v_%v.tar.gz", version, version[1:], goos, arch)
	assets, _ := result["assets"].([]interface{})

	for _, tmp := range assets {
		asset, _ := tmp.(map[string]interface{})
		if url == asset["browser_download_url"] {
			size = uint(asset["size"].(float64))
			break
		}
	}

	return
}

// WriteCounter progress counter
type WriteCounter struct {
	Count, Total uint
	last         uint
}

// Print print progress
func (w *WriteCounter) Print() {
	fmt.Printf("\rProgress: %v/%v KB  Speed: %v KB/s  Remain: %.0f s           ",
		w.Count/1024, w.Total/1024, (w.Count-w.last)/1024, float64(w.Total-w.Count)/float64(w.Count-w.last))

	w.last = w.Count
}

func (w *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	w.Count += uint(n)

	return n, nil
}

func upgrade(url, exePath string, size uint) (err error) {
	updateDir := filepath.Dir(exePath)

	oldPath := filepath.Join(updateDir, fmt.Sprintf(".%s.old", filepath.Base(exePath)))
	color.Cyan("Move the old one to %v", oldPath)
	if err = os.Rename(exePath, oldPath); err != nil {
		return
	}

	defer func() {
		if err != nil {
			color.Cyan("Move the old one back")
			if e := os.Rename(oldPath, exePath); e != nil {
				color.Red(e.Error())
			}
		} else {
			color.Cyan("Remove the old one")
			if e := os.Remove(oldPath); e != nil {
				color.Red(e.Error() + "\nYou could remove it manually")
			}
		}
	}()

	color.Cyan("Download %v", url)
	counter := &WriteCounter{Count: 0, Total: size, last: 0}
	counter.Print()

	ticker := time.NewTicker(time.Second)
	go func() {
		for range ticker.C {
			counter.Print()
		}
	}()

	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(io.TeeReader(resp.Body, counter))
	ticker.Stop()
	counter.Print()
	fmt.Println()

	if err != nil {
		return
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	if err != nil {
		return
	}

	err = (func() (err error) {
		for {
			hdr, err := tr.Next()
			if err != nil {
				return err
			}

			fileinfo := hdr.FileInfo()
			if strings.HasPrefix(fileinfo.Name(), "cf") {
				color.Green("Executable file found. [name=%s, size=%.2fMiB]", fileinfo.Name(), float64(fileinfo.Size())/1024/1024)
				return nil
			}
		}
	})()
	if err != nil {
		return err
	}

	newPath := filepath.Join(updateDir, fmt.Sprintf(".%s.new", filepath.Base(exePath)))
	color.Cyan("Save the new one to %v", newPath)

	fw, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer fw.Close()

	_, err = io.Copy(fw, tr)
	if err != nil {
		return err
	}

	if err = os.Rename(newPath, exePath); err != nil {
		color.Cyan("Delete the new one %v", newPath)

		if e := os.Remove(newPath); e != nil {
			color.Red(e.Error())
		}
	}

	return
}

// Upgrade itself
func Upgrade() (err error) {
	color.Cyan("Checking version")

	latest, note, ptime, url, size, err := getLatest()
	if err != nil {
		return
	}

	version := Args.Version
	if !less(version, latest) {
		color.Green("Current version %v is the latest", version)
		return
	}

	color.Red("Current version is %v", version)
	color.Green("The latest version is %v, published at %v", latest, ptime)
	fmt.Println(note)

	if !util.YesOrNo("Do you want to upgrade (y/n)? ") {
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		return
	}

	if exePath, err = filepath.EvalSymlinks(exePath); err != nil {
		return
	}

	if err = upgrade(url, exePath, size); err != nil {
		return
	}

	color.Green("Successfully updated to version %v", latest)

	return
}

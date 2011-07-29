package main

import (
	"bufio"
	"bytes"
	"exec"
	"log"
	"flag"
	"strconv"
	"strings"
	"os"
	"fmt"
)

type Port struct {
	Origin string
	PortRevision int
	ToBump bool
	HasSoversion bool
}

// Flags
var portsPath string
var portOrigin string
var libName string
var libOldVersion string
var libNewVersion string
var userName string

var oldSoname []byte
var newSoname []byte

func visitCategory(category string) []*Port {
	var ports []*Port
	categoryPath := portsPath + "/" + category
	cat, err := os.Open(categoryPath)
	if err != nil {
		log.Fatal(err)
	}
	dirs, err := cat.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range dirs {
		if d.IsDirectory() {
			p := visitPort(category + "/" + d.Name)
			if p != nil && (p.ToBump || p.HasSoversion) {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

func visitPort(origin string) *Port {
	var p *Port

	if origin == portOrigin {
		return nil
	}

	foundRef := false
	foundSoversion := false

	path := portsPath + "/" + origin + "/Makefile"
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err == os.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		if bytes.Index(line, []byte(portOrigin)) != -1 {
			foundRef = true
			if bytes.Index(line, oldSoname) != -1 {
				foundSoversion = true
			}
		}
	}

	if foundRef {
		p = getPort(origin)
		p.HasSoversion = foundSoversion
	}

	return p
}

func getPort(origin string) *Port {
	var lines []string
	args := []string {
		"-C", portsPath + "/" + origin,
		"-V", "LIB_DEPENDS",
		"-V", "BUILD_DEPENDS",
		"-V", "RUN_DEPENDS",
		"-V", "PORTREVISION",
	}

	p := &Port{Origin: origin}
	cmd := exec.Command("make", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	r := bufio.NewReader(out)
	for {
		line, err := r.ReadString('\n')
		if err == os.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		lines = append(lines, line)
	}

	if len(lines) != 4 {
		log.Fatal("Wut? Wut?")
	}

	// If it is in lib depends
	if strings.Index(lines[0], portOrigin) != -1 {
		p.ToBump = true
	}

	// If it is in build AND run depends, it might also link to the lib
	if strings.Index(lines[1], portOrigin) != -1 &&
		strings.Index(lines[1], portOrigin) != -1 {
		p.ToBump = true
	}

	p.PortRevision, err = strconv.Atoi(strings.TrimSpace(lines[3]))
	if err != nil {
		log.Fatal(err)
	}

	return p
}

func updateMakefile(p *Port) {
	path := "ports/" + p.Origin + "/Makefile"
	err := os.Rename(path, path + ".orig")
	if err != nil {
		log.Fatal(err)
	}

	i, err := os.Open(path + ".orig")
	if err != nil {
		log.Fatal(err)
	}
	defer i.Close()
	r := bufio.NewReader(i)

	o, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer o.Close()
	w := bufio.NewWriter(o)

	for {
		line, err := r.ReadBytes('\n')
		if err == os.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}

		if p.ToBump && bytes.Index(line, []byte("PORTREVISION")) == 0 {
			continue
		}

		if p.HasSoversion {
			line = bytes.Replace(line, oldSoname, newSoname, -1)
		}

		w.Write(line)

		if p.ToBump {
			if bytes.Index(line, []byte("PORTVERSION")) == 0 ||
			   bytes.Index(line, []byte("DISTVERSION")) == 0 {
				fmt.Fprintf(w, "PORTREVISION=\t%d\n", p.PortRevision + 1)
			}
		}

	}

	w.Flush()
}

func checkOut(ports []*Port) {
	args := []string {
		"-d",
		userName + "@pcvs.FreeBSD.org:/home/pcvs",
		"co",
		"ports/UPDATING",
	}

	for _, port := range ports {
		args = append(args, "ports/" + port.Origin)
	}
	cmd := exec.Command("cvs", args...)
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func isCategory(name string) bool {
	skip := []string{"Mk", "Templates", "Tools", "distfiles", "packages"}
	for _, s := range skip {
		if name == s {
			return false
		}
	}
	return true
}

func main() {
	flag.StringVar(&portOrigin, "portOrigin", "", "port holding the lib")
	flag.StringVar(&libName, "libName", "", "library name")
	flag.StringVar(&libOldVersion, "libOldVersion", "", "old version")
	flag.StringVar(&libNewVersion, "libNewVersion", "", "new version")
	flag.StringVar(&portsPath, "portsPath", "/usr/ports", "where ports live")
	flag.StringVar(&userName, "userName", os.Getenv("USER"), "cvs user")
	flag.Parse()

	if len(portOrigin) == 0 || len(libName) == 0 || len(libOldVersion) == 0 ||
		len(libNewVersion) == 0 {
		log.Fatal("missing args")
	}

	var ports []*Port

	oldSoname = []byte(libName + "." + libOldVersion)
	newSoname = []byte(libName + "." + libNewVersion)

	// Scan
	fmt.Println("Scanning the ports tree...")
	root, err := os.Open(portsPath)
	if err != nil {
		fmt.Println(err)
	}
	defer root.Close()
	dirs, err := root.Readdir(-1)
	for _, d := range dirs {
		if !d.IsDirectory() || !isCategory(d.Name) {
			continue
		}
		p := visitCategory(d.Name)
		ports = append(ports, p...)
	}

	// Checkout
	fmt.Println("Checkout'ing ports to bump...")
	checkOut(ports)

	fmt.Println("Updating Makefiles...")
	for _, port := range ports {
		updateMakefile(port)
	}
}

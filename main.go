package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Input struct {
	Dir   string
	Dry   bool
	Force bool
}

type Link struct {
	Src  string
	Dest string
}

func (l Link) String() string {
	return fmt.Sprintf("%v -> %v", l.Src, l.Dest)
}

func (l Link) cmd(force bool) string {
	if force {
		return fmt.Sprintf("ls -sf %v %v", l.Src, l.Dest)
	}
	return fmt.Sprintf("ls -s %v %v", l.Src, l.Dest)
}

func (l *Link) Clean() {
	l.Src = CleanPath(l.Src)
	l.Dest = CleanPath(l.Dest)
}

func CleanPath(path string) string {
	path = filepath.Clean(path)
	hasLeadingSlash := strings.HasPrefix(path, "/")
	var cleanPath []string
	for _, e := range strings.Split(path, "/") {
		if strings.HasPrefix(e, "$") {
			e = os.Getenv(strings.TrimPrefix(e, "$"))
		}
		cleanPath = append(cleanPath, e)
	}
	path = filepath.Join(cleanPath...)
	if hasLeadingSlash {
		path = "/" + path
	}
	return path
}

func (l *Link) Symlink(force bool) error {
	if force {
		err := os.Remove(l.Dest)
		if err != nil {
			return err
		}
	}
	return os.Symlink(l.Src, l.Dest)
}

type DotDir struct {
	Path     string
	LinkFile string
}

func (d DotDir) Links() (links []Link, err error) {
	f, err := os.Open(d.LinkFile)
	if err != nil {
		log.Printf("Error openeing link file %v: %v", d.LinkFile, err)
		return nil, err
	}
	defer f.Close()

	var m map[string]string
	err = json.NewDecoder(f).Decode(&m)
	if err != nil {
		log.Printf("Error parsing link file %v: %v", d.LinkFile, err)
		return nil, err
	}
	for src, dest := range m {
		src = filepath.Join(d.Path, src)
		link := Link{Src: src, Dest: dest}
		link.Clean()
		links = append(links, link)
	}
	return
}

type Bootstrap struct {
	DotDirs []DotDir
}

func (b *Bootstrap) AddDir(dir, links string) {
	b.DotDirs = append(b.DotDirs, DotDir{
		Path:     dir,
		LinkFile: links,
	})
}

func (b *Bootstrap) Walk(dir string) error {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		// Check for link file
		if info.Name() == LinkFile {
			d, _ := filepath.Split(path)
			b.AddDir(d, path)
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

type LinkResult struct {
	Link Link
	Err  error
}

func (r LinkResult) String() string {
	if r.Err != nil {
		return fmt.Sprintf("%v: %v", r.Err, r.Link)
	}
	return r.Link.String()
}

func (b *Bootstrap) Link(links chan Link, errors chan error) {
	toLinks := func(l Link) {
		if links != nil {
			links <- l
		}
	}
	toErrors := func(e error) {
		if errors != nil {
			errors <- e
		}
	}
	wg := &sync.WaitGroup{}
	for _, dotDir := range b.DotDirs {
		wg.Add(1)
		go func(dotDir DotDir) {
			defer wg.Done()
			links, err := dotDir.Links()
			if err != nil {
				toErrors(err)
			}
			for _, link := range links {
				toLinks(link)
			}
		}(dotDir)
	}
	wg.Wait()
}

const DotEnv = "DOT"
const LinkFile = "links.json"

func main() {
	i := Input{
		Dir:   os.Getenv(DotEnv),
		Dry:   false,
		Force: false,
	}
	if i.Dir == "" {
		i.Dir = "../"
	}
	flag.StringVar(&i.Dir, "dir", i.Dir, "The dotfiles source.")
	flag.BoolVar(&i.Dry, "dry", i.Dry, "Only print out the changes.")
	flag.BoolVar(&i.Force, "force", i.Force, "Overwrite existing links.")
	flag.Parse()

	b := &Bootstrap{}

	dir, err := filepath.Abs(i.Dir)
	if err != nil {
		log.Fatal(err)
	}
	err = b.Walk(dir)
	if err != nil {
		log.Fatal(err)
	}

	links := make(chan Link)
	errors := make(chan error)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	messages := map[string][]string{}
	go func(messages map[string][]string) {
		defer wg.Done()
		var linksDone, errorsDone bool
		for !linksDone || !errorsDone {
			select {
			case link, ok := <-links:
				if !ok {
					linksDone = true
					continue
				}
				if i.Dry {
					a := messages["Commands"]
					messages["Commands"] = append(a, link.cmd(i.Force))
					continue
				}
				err := link.Symlink(i.Force)
				if err != nil {
					if lerr, ok := err.(*os.LinkError); ok {
						a := messages["Failures"]
						messages["Failures"] = append(a, fmt.Sprintf("%v: %v", lerr.Err, link))
					}
					continue
				}
				a := messages["Successes"]
				messages["Successes"] = append(a, link.String())
			case err, ok := <-errors:
				if !ok {
					errorsDone = true
					continue
				}
				a := messages["Errors"]
				messages["Errors"] = append(a, err.Error())
			}
		}
	}(messages)

	b.Link(links, errors)
	close(links)
	close(errors)
	wg.Wait()
	for header, msgs := range messages {
		if len(messages) > 1 {
			fmt.Println(header + ":")
		}
		fmt.Println(strings.Join(msgs, "\n"))
	}
	fmt.Println("Changes will take effect after sourcing your .*shrc")
}

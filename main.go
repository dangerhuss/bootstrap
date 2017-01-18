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

// Input holds the user settable values.
type Input struct {
	Dir   string
	Dry   bool
	Force bool
}

// Link is a single symlink. A source and destination are required
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

// Clean replaces the environment variables in the source and destination paths with the values.
func (l *Link) Clean() {
	l.Src = cleanPath(l.Src)
	l.Dest = cleanPath(l.Dest)
}

func cleanPath(path string) string {
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

// Symlink creates a symlink using the Src and Dest. Dest will be removed if force is set.
func (l *Link) Symlink(force bool) error {
	if force {
		err := os.Remove(l.Dest)
		if err != nil {
			return err
		}
	}
	return os.Symlink(l.Src, l.Dest)
}

// DotDir is a directory containing a links file. The paths in the links file, if not absolute, will be relative to the Path attribute.
type DotDir struct {
	Path     string
	LinkFile string
}

// Links parses a list of links from the links file. The found links will be cleaned and returned. An error will be returned if parsing the links file fails.
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

// Bootstrap manages a list of files that need to be symlinked.
type Bootstrap struct {
	DotDirs []DotDir
}

// AddDir adds a DotDir to the DotDirs given the directory path and path to the links file.
func (b *Bootstrap) AddDir(dir, links string) {
	b.DotDirs = append(b.DotDirs, DotDir{
		Path:     dir,
		LinkFile: links,
	})
}

// Walk traverses the specified directory. Any directories found containing a links file will be added to the DotDirs attribute. An error will be returned if the walking fails.
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

// Link adds the links from each of the DotDirs to the links chan. If an error occurs while getting a DotDirs links, the error will be added to the errors chan.
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

// DotEnv is the name of the environment variable signifying the location of the dotfiles needing bootstrapping.
const DotEnv = "DOT"

// LinkFile is the name the file describing symlinks relative to the current directory.
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


	dir, err := filepath.Abs(i.Dir)
	if err != nil {
		log.Fatal(err)
	}

	// Create and populate the Bootstrap DotDirs
	b := &Bootstrap{}
	err = b.Walk(dir)
	if err != nil {
		log.Fatal(err)
	}

	// Create the needed chans
	links := make(chan Link)
	errors := make(chan error)

	wg := new(sync.WaitGroup)
	wg.Add(1) // Add 1 for the single go routine listening on the above chans
	messages := map[string][]string{}

	// Spawn a go routine to create the desired links
	go func(messages map[string][]string) {
		defer wg.Done()
		var linksDone, errorsDone bool
		for !linksDone || !errorsDone {
			select {
			case link, ok := <-links:
				if !ok {
					// The links chan has been closed.
					linksDone = true
					continue
				}

				if i.Dry {
					// Add the ln commands to the messages map.
					a := messages["Commands"]
					messages["Commands"] = append(a, link.cmd(i.Force))
					continue
				}

				// Write the symlink. Use the user specified force flag.
				err := link.Symlink(i.Force)
				if err != nil {
					if lerr, ok := err.(*os.LinkError); ok {
						a := messages["Failures"]
						messages["Failures"] = append(a, fmt.Sprintf("%v: %v", lerr.Err, link))
					}
					continue
				}
				// Add the newly created Link string to the messages map.
				a := messages["Successes"]
				messages["Successes"] = append(a, link.String())
			case err, ok := <-errors:
				if !ok {
					// The errors chan has been closed
					errorsDone = true
					continue
				}
				// Add the bootstrap error to the messages map.
				a := messages["Errors"]
				messages["Errors"] = append(a, err.Error())
			}
		}
	}(messages)

	// Kick off the links method.
	b.Link(links, errors)

	// Links only returns once all the links or errors
	// have been added to the respective chan.We can
	// safley close the links and errors chans.
	close(links)
	close(errors)
	// Wait for all the symlinks to be created.
	wg.Wait()
	// Print out all the messages
	for header, msgs := range messages {
		if len(messages) > 1 {
			fmt.Println(header + ":")
		}
		fmt.Println(strings.Join(msgs, "\n"))
	}
	fmt.Println("Changes will take effect after sourcing your .*shrc")
}

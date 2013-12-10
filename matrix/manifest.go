package matrix

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AssetMap struct {
	Dir   *Dir
	Files map[string]*File
}

type Manifest struct {
	AssetRoots      []*Dir
	InputDirs       []string
	OutputDir       string
	DirPathMapping  map[string]*Dir
	FilePathMapping map[string]*File
	NameMapping     map[string]*AssetMap
	fileHandlers    []*FileHandler
	log             *log.Logger
}

func NewManifest(inputDirs []string, outputDir string, logOut io.Writer) *Manifest {
	manifest := &Manifest{InputDirs: inputDirs, OutputDir: outputDir, DirPathMapping: make(map[string]*Dir), FilePathMapping: make(map[string]*File), NameMapping: make(map[string]*AssetMap)}
	manifest.log = log.New(logOut, "matrix: ", 0)
	return manifest
}

func (manifest *Manifest) AddDir(dir *Dir) {
	manifest.DirPathMapping[dir.Path()] = dir

	if manifest.NameMapping[dir.Name()] == nil {
		manifest.NameMapping[dir.Name()] = &AssetMap{Dir: dir}
	} else {
		manifest.NameMapping[dir.Name()].Dir = dir
	}
}

func (manifest *Manifest) AddFile(file *File) {
	manifest.FilePathMapping[file.Path()] = file

	if manifest.NameMapping[file.Name()] == nil {
		manifest.NameMapping[file.Name()] = &AssetMap{}
	}
	if manifest.NameMapping[file.Name()].Files == nil {
		manifest.NameMapping[file.Name()].Files = make(map[string]*File)
	}
	manifest.NameMapping[file.Name()].Files[file.Ext()] = file
}

func (manifest *Manifest) FindDirName(name string) *Dir {
	assetMap := manifest.NameMapping[name]
	if assetMap == nil {
		return nil
	}

	return assetMap.Dir
}

func (manifest *Manifest) FindFileName(name string, ext string) *File {
	assetMap := manifest.NameMapping[name]
	if assetMap == nil {
		return nil
	}

	files := assetMap.Files
	if files == nil {
		return nil
	}

	if ext != "" {
		return files[ext]
	} else {
		for _, file := range files {
			return file
		}
		return nil
	}
}

func (manifest *Manifest) ScanInputDirs() error {
	manifest.AssetRoots = make([]*Dir, len(manifest.InputDirs))
	for i, path := range manifest.InputDirs {
		dir, err := NewDir(path, manifest, nil)
		if err != nil {
			return err
		}

		if err := dir.Scan(); err != nil {
			return err
		}

		manifest.AssetRoots[i] = dir
	}

	return nil
}

func (manifest *Manifest) EvaluateDirectives() error {
	for _, assetMap := range manifest.NameMapping {
		if assetMap == nil {
			continue
		}
		if assetMap.Files == nil {
			continue
		}

		for _, file := range assetMap.Files {
			if err := file.EvaluateDirectives(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (manifest *Manifest) ConfigureHandlers() error {
	// Build initial handler chains
	manifest.fileHandlers = make([]*FileHandler, 0, len(manifest.FilePathMapping))
	for _, file := range manifest.FilePathMapping {
		fileHandler := NewFileHandler(file.Ext())
		fileHandler.File = file
		file.FileHandler = fileHandler
		manifest.fileHandlers = append(manifest.fileHandlers, fileHandler)
	}

	// Build lists of parent/child file handlers
	for _, file := range manifest.FilePathMapping {
		selfAdded := false
		for _, directive := range file.Directives {
			for _, f := range directive.Files() {
				fileHandler := f.FileHandler
				if fileHandler == file.FileHandler {
					selfAdded = true
				}
				file.FileHandler.AddFileHandler(fileHandler)
			}
		}

		if !selfAdded {
			file.FileHandler.AddFileHandler(file.FileHandler)
		}
	}

	// Sort file handlers by len(fh.ParentHandlers) (most to least)
	sort.Sort(byLenParentHandlersReversed(manifest.fileHandlers))

	// Insert concatenation handlers
	for _, fh := range manifest.fileHandlers {
		if err := fh.MergeWithParents(); err != nil {
			return err
		}
	}

	return nil
}

func (manifest *Manifest) outFilePath(name string, exts []string) (string, error) {
	path, err := filepath.Abs(filepath.Join(manifest.OutputDir, name))
	if err != nil {
		return "", err
	}
	parts := append([]string{path}, exts...)
	return strings.Join(parts, "."), nil
}

func (manifest *Manifest) WriteOutput() error {
	// Loop through fileHandlers in reverse order (least to most ParentHandlers)
	for i := len(manifest.fileHandlers); i > 0; i-- {
		fh := manifest.fileHandlers[i-1]

		// Don't output files included as part of others
		if len(fh.ParentHandlers) > 0 {
			continue
		}

		manifest.log.Printf("Processing %s\n", fh.File.Name())

		f, err := os.Open(fh.File.Path())
		if err != nil {
			return err
		}

		out := new(bytes.Buffer)
		name, exts, err := fh.Handle(f, out, fh.File.Name(), fh.File.Exts())
		if closeErr := f.Close(); closeErr != nil {
			return closeErr
		}
		if err != nil {
			return err
		}

		outPath, err := manifest.outFilePath(name, exts)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.Create(outPath)
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, out)
		if err != nil {
			return err
		}
	}
	return nil
}

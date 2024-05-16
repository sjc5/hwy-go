package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/sjc5/kit/pkg/rpc"
)

type BuildOptions struct {
	IsDev             bool
	ClientEntry       string
	PagesSrcDir       string
	HashedOutDir      string
	UnhashedOutDir    string
	ClientEntryOut    string
	UsePreactCompat   bool
	DataFuncsMap      DataFuncsMap
	GeneratedTSOutDir string
}

func walkPages(pagesSrcDir string) []JSONSafePath {
	var paths []JSONSafePath
	filepath.WalkDir(pagesSrcDir, func(patternArg string, d fs.DirEntry, err error) error {
		cleanPatternArg := filepath.Clean(strings.TrimPrefix(patternArg, pagesSrcDir))
		isPageFile := strings.Contains(cleanPatternArg, ".ui.")
		if !isPageFile {
			return nil
		}
		ext := filepath.Ext(cleanPatternArg)
		preExtDelineator := ".ui"
		pattern := strings.TrimSuffix(cleanPatternArg, preExtDelineator+ext)
		isIndex := false
		patternToSplit := strings.TrimPrefix(pattern, "/")

		// Clean out double underscore segments
		segmentsInitWithDubUnderscores := strings.Split(patternToSplit, "/")
		segmentsInit := make([]string, 0, len(segmentsInitWithDubUnderscores))
		for _, segment := range segmentsInitWithDubUnderscores {
			if strings.HasPrefix(segment, "__") {
				continue
			}
			segmentsInit = append(segmentsInit, segment)
		}

		segments := make([]SegmentObj, len(segmentsInit))
		for i, segmentStr := range segmentsInit {
			isSplat := false
			if segmentStr == "$" {
				isSplat = true
			}
			if segmentStr == "_index" {
				segmentStr = ""
				isIndex = true
			}
			segmentType := "normal"
			if isSplat {
				segmentType = "splat"
			} else if strings.HasPrefix(segmentStr, "$") {
				segmentType = "dynamic"
			} else if isIndex {
				segmentType = "index"
			}
			segments[i] = SegmentObj{
				SegmentType: segmentType,
				Segment:     segmentStr,
			}
		}
		segmentStrs := make([]string, len(segments))
		for i, segment := range segments {
			segmentStrs[i] = segment.Segment
		}
		SrcPath := filepath.Join(pagesSrcDir, pattern) + preExtDelineator + ext
		truthySegments := []string{}
		for _, segment := range segmentStrs {
			if segment != "" {
				truthySegments = append(truthySegments, segment)
			}
		}
		patternToUse := "/" + strings.Join(truthySegments, "/")
		if patternToUse != "/" && strings.HasSuffix(patternToUse, "/") {
			patternToUse = strings.TrimSuffix(patternToUse, "/")
		}
		pathType := PathTypeStaticLayout
		if isIndex {
			pathType = PathTypeIndex
			if patternToUse == "/" {
				patternToUse += "_index"
			} else {
				patternToUse += "/_index"
			}
		} else if segments[len(segments)-1].SegmentType == "splat" {
			pathType = PathTypeNonUltimateSplat
		} else if segments[len(segments)-1].SegmentType == "dynamic" {
			pathType = PathTypeDynamicLayout
		}
		if patternToUse == "/$" {
			pathType = PathTypeUltimateCatch
		}
		paths = append(paths, JSONSafePath{
			Pattern:  patternToUse,
			Segments: &segmentStrs,
			PathType: pathType,
			SrcPath:  SrcPath,
		})
		return nil
	})
	return paths
}

func writePathsToDisk(pagesSrcDir string, pathsJSONOut string) error {
	paths := walkPages(pagesSrcDir)
	err := os.MkdirAll(filepath.Dir(pathsJSONOut), os.ModePerm)
	if err != nil {
		return err
	}
	pathsAsJSON, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	err = os.WriteFile(pathsJSONOut, pathsAsJSON, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func readPathsFromDisk(path string) (*[]JSONSafePath, error) {
	paths := []JSONSafePath{}
	asdf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(asdf, &paths)
	if err != nil {
		return nil, err
	}
	return &paths, nil
}

type ImportPath = string

type MetafileImport struct {
	Path ImportPath `json:"path"`
	Kind string     `json:"kind"`
}

type MetafileJSON struct {
	Outputs map[ImportPath]struct {
		Imports    []MetafileImport `json:"imports"`
		EntryPoint string           `json:"entryPoint"`
	} `json:"outputs"`
}

type PathsFile struct {
	Paths           []JSONSafePath `json:"paths"`
	ClientEntryDeps []ImportPath   `json:"clientEntryDeps"`
	BuildID         string         `json:"buildID"`
}

func GenerateTypeScript(opts BuildOptions) error {
	var routeDefs []rpc.RouteDef

	for k, v := range opts.DataFuncsMap {
		if v.Loader != nil {
			routeDefs = append(routeDefs, rpc.RouteDef{
				Key:    k,
				Type:   rpc.TypeQuery,
				Output: v.LoaderOutput,
			})
		}
		if v.Action != nil {
			routeDefs = append(routeDefs, rpc.RouteDef{
				Key:    k,
				Type:   rpc.TypeMutation,
				Input:  v.ActionInput,
				Output: v.ActionOutput,
			})
		}
	}

	err := rpc.GenerateTypeScript(rpc.Opts{
		OutDest:   opts.GeneratedTSOutDir,
		RouteDefs: routeDefs,
	})

	return err
}

func Build(opts BuildOptions) error {
	startTime := time.Now()
	buildID := fmt.Sprintf("%d", startTime.Unix())
	Log.Infof("new build id: %s", buildID)

	pathsJSONOut := filepath.Join(opts.UnhashedOutDir, "hwy_paths.json")
	err := writePathsToDisk(opts.PagesSrcDir, pathsJSONOut)
	if err != nil {
		return err
	}
	env := "production"
	if opts.IsDev {
		env = "development"
	}
	sourcemap := api.SourceMapNone
	if opts.IsDev {
		sourcemap = api.SourceMapLinked
	}
	paths, err := readPathsFromDisk(pathsJSONOut)
	if err != nil {
		return err
	}
	entryPoints := make([]string, 0, len(*paths)+1)
	entryPoints = append(entryPoints, opts.ClientEntry)
	for _, path := range *paths {
		entryPoints = append(entryPoints, path.SrcPath)
	}
	// clear hashed out dir
	// __TODO consider using a hwy_internal dir instead of in root
	err = os.RemoveAll(opts.HashedOutDir)
	if err != nil {
		return err
	}
	alias := map[string]string{}
	if opts.UsePreactCompat {
		alias["react"] = "preact/compat"
		alias["react-dom/test-utils"] = "preact/test-utils"
		alias["react-dom"] = "preact/compat"
		alias["react/jsx-runtime"] = "preact/jsx-runtime"
	}
	result := api.Build(api.BuildOptions{
		Format:      api.FormatESModule,
		Bundle:      true,
		TreeShaking: api.TreeShakingTrue,
		Define: map[string]string{
			"process.env.NODE_ENV": "\"" + env + "\"",
		},
		Sourcemap:         sourcemap,
		MinifyWhitespace:  !opts.IsDev,
		MinifyIdentifiers: !opts.IsDev,
		MinifySyntax:      !opts.IsDev,
		EntryPoints:       entryPoints,
		Outdir:            opts.HashedOutDir,
		Platform:          api.PlatformBrowser,
		Splitting:         true,
		ChunkNames:        "hwy_chunk__[hash]",
		Write:             true,
		EntryNames:        "hwy_entry__[hash]",
		Metafile:          true,
		Alias:             alias,
	})
	if len(result.Errors) > 0 {
		return errors.New(result.Errors[0].Text)
	}
	metafileJSONMap := MetafileJSON{}
	err = json.Unmarshal([]byte(result.Metafile), &metafileJSONMap)
	if err != nil {
		return err
	}

	hwyClientEntry := ""
	hwyClientEntryDeps := []string{}
	for key, output := range metafileJSONMap.Outputs {
		entryPoint := output.EntryPoint
		deps, err := findAllDependencies(&metafileJSONMap, key)
		if err != nil {
			return err
		}
		if opts.ClientEntry == entryPoint {
			hwyClientEntry = filepath.Base(key)
			depsWithoutClientEntry := make([]string, 0, len(deps)-1)
			for _, dep := range deps {
				if dep != hwyClientEntry {
					depsWithoutClientEntry = append(depsWithoutClientEntry, dep)
				}
			}
			hwyClientEntryDeps = depsWithoutClientEntry
		} else {
			for i, path := range *paths {
				if path.SrcPath == entryPoint {
					(*paths)[i].OutPath = filepath.Base(key)
					(*paths)[i].Deps = &deps
				}
			}
		}
	}
	pathsAsJSON, err := json.Marshal(PathsFile{
		Paths:           *paths,
		ClientEntryDeps: hwyClientEntryDeps,
		BuildID:         buildID,
	})
	if err != nil {
		return err
	}
	err = os.WriteFile(pathsJSONOut, pathsAsJSON, os.ModePerm)
	if err != nil {
		return err
	}

	// Mv file at path stored in hwyClientEntry var to ../ in OutDir
	clientEntryFileBytes, err := os.ReadFile(filepath.Join(opts.HashedOutDir, hwyClientEntry))
	if err != nil {
		return err
	}

	err = os.WriteFile(filepath.Join(opts.ClientEntryOut, "hwy_client_entry.js"), clientEntryFileBytes, os.ModePerm)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(opts.HashedOutDir, hwyClientEntry))
	if err != nil {
		return err
	}

	Log.Infof("build completed in %s", time.Since(startTime))
	return nil
}

func findAllDependencies(metafile *MetafileJSON, entry ImportPath) ([]ImportPath, error) {
	seen := make(map[ImportPath]bool)
	var result []ImportPath

	var recurse func(path ImportPath)
	recurse = func(path ImportPath) {
		if seen[path] {
			return
		}
		seen[path] = true
		result = append(result, path)

		if output, exists := metafile.Outputs[path]; exists {
			for _, imp := range output.Imports {
				recurse(imp.Path)
			}
		}
	}

	recurse(entry)

	cleanResults := make([]ImportPath, 0, len(result)+1)
	for _, res := range result {
		cleanResults = append(cleanResults, filepath.Base(res))
	}
	if !slices.Contains(cleanResults, filepath.Base(entry)) {
		cleanResults = append(cleanResults, filepath.Base(entry))
	}
	return cleanResults, nil
}

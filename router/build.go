package router

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

type BuildOptions struct {
	IsDev          bool
	ClientEntry    string
	PagesSrcDir    string
	HashedOutDir   string
	UnhashedOutDir string
	ClientEntryOut string
}

// __TODO -- allow for dirs starting with double underscore to be ignored

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
		segmentsInit := strings.Split(patternToSplit, "/")
		segments := make([]SegmentObj, len(segmentsInit))
		for i, segmentStr := range segmentsInit {
			newSegment := strings.Replace(segmentStr, "$", ":", -1)
			isSplat := false
			if newSegment == ":" {
				newSegment = SplatSegment
				isSplat = true
			}
			if newSegment == "_index" {
				newSegment = ""
				isIndex = true
			}
			segmentType := "normal"
			if isSplat {
				segmentType = "splat"
			} else if strings.HasPrefix(newSegment, ":") {
				segmentType = "dynamic"
			} else if isIndex {
				segmentType = "index"
			}
			segments[i] = SegmentObj{
				SegmentType: segmentType,
				Segment:     newSegment,
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
		} else if segments[len(segments)-1].SegmentType == "splat" {
			pathType = PathTypeNonUltimateSplat
		} else if segments[len(segments)-1].SegmentType == "dynamic" {
			pathType = PathTypeDynamicLayout
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

func Build(opts BuildOptions) error {
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
		sourcemap = api.SourceMapInline
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
	})
	if len(result.Errors) > 0 {
		return errors.New(result.Errors[0].Text)
	}
	metafileJSONMap := map[string]interface{}{}
	err = json.Unmarshal([]byte(result.Metafile), &metafileJSONMap)
	if err != nil {
		return err
	}
	hwyClientEntry := ""
	for key, output := range metafileJSONMap["outputs"].(map[string]interface{}) {
		entryPoint := esbuildOutputToEntryPointStr(output)
		if opts.ClientEntry == entryPoint {
			hwyClientEntry = filepath.Base(key)
		} else {
			for i, path := range *paths {
				if path.SrcPath == entryPoint {
					(*paths)[i].OutPath = filepath.Base(key)
				}
			}
		}
	}
	pathsAsJSON, err := json.Marshal(*paths)
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
	return nil
}

// 1 -- Generate TS types from Go loaders, actions, and head functions
// 2 -- Run Hwy build of the frontend
// 3 -- Run Kiruna build of the backend

func esbuildOutputToEntryPointStr(output interface{}) string {
	if outputMap, ok := output.(map[string]interface{}); ok {
		entryPointVal, ok := outputMap["entryPoint"]
		if ok {
			if entryPointStr, ok := entryPointVal.(string); ok {
				return entryPointStr
			}
		}
	}
	return ""
}
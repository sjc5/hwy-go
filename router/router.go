package router

import (
	"errors"
	"html/template"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
)

type SegmentObj struct {
	SegmentType string
	Segment     string
}

// __TODO -- need to handle redirects from loaders / actions

const SplatSegment = ":catch*"

var PathTypeUltimateCatch = "ultimate-catch"
var PathTypeIndex = "index"
var PathTypeStaticLayout = "static-layout"
var PathTypeDynamicLayout = "dynamic-layout"
var PathTypeNonUltimateSplat = "non-ultimate-splat"

type Path struct {
	Pattern   string
	Segments  *[]string
	PathType  string
	OutPath   string
	SrcPath   string
	DataFuncs *DataFuncs
}

type JSONSafePath struct {
	Pattern  string
	Segments *[]string
	PathType string
	OutPath  string
	SrcPath  string
}

type HeadBlock struct {
	Tag        string
	Attributes map[string]string
	Title      string
}

type Loader func(DataProps) (interface{}, error)
type Action func(DataProps) (interface{}, error)
type Head func(HeadProps) (*[]HeadBlock, error)

type DataProps struct {
	Request       *http.Request
	Params        *map[string]string
	SplatSegments *[]string
}

type HeadProps struct {
	DataProps
	LoaderData interface{}
	ActionData interface{}
}

type DataFuncs struct {
	Loader Loader
	Action Action
	Head   Head
}

type ActivePathData struct {
	MatchingPaths               *[]*DecoratedPath
	LoadersData                 *[]interface{}
	ImportURLs                  *[]string
	OutermostErrorBoundaryIndex int
	ActionData                  *[]interface{}
	ActiveHeads                 *[]Head
	SplatSegments               *[]string
	Params                      *map[string]string
}

type matcherOutput struct {
	path               string
	pattern            string
	matches            bool
	params             *map[string]string
	score              int
	realSegmentsLength int
}

type GroupedBySegmentLength map[int]*[]*MatchingPath
type DataFuncsMap = map[string]DataFuncs

type MatchStrength struct {
	Score              int
	RealSegmentsLength int
}

type MatchingPath struct {
	Score              int
	RealSegmentsLength int
	Segments           *[]string
	PathType           string
	DataFuncs          *DataFuncs
	OutPath            string
	Params             *map[string]string
}

type DecoratedPath struct {
	DataFuncs *DataFuncs
	PathType  string // only needed for testing __TODO
}

type gmpdItem struct {
	SplatSegments               *[]string
	Params                      *map[string]string
	FullyDecoratedMatchingPaths *[]*DecoratedPath
	ImportURLs                  *[]string
}

type GetRouteDataOutput struct {
	Title                       string                   `json:"title"`
	MetaHeadBlocks              *[]*HeadBlock            `json:"metaHeadBlocks"`
	RestHeadBlocks              *[]*HeadBlock            `json:"restHeadBlocks"`
	LoadersData                 *[]interface{}           `json:"loadersData"`
	ImportURLs                  *[]string                `json:"importURLs"`
	OutermostErrorBoundaryIndex int                      `json:"outermostErrorBoundaryIndex"`
	SplatSegments               *[]string                `json:"splatSegments"`
	Params                      *map[string]string       `json:"params"`
	ActionData                  *[]interface{}           `json:"actionData"`
	AdHocData                   *map[string]*interface{} `json:"adHocData"`
	BuildID                     string                   `json:"buildID"`
}

var instancePaths *[]Path

type Hwy struct {
	DefaultHeadBlocks []HeadBlock
	GetBasePaths      func() ([]Path, error)
	DataFuncsMap      DataFuncsMap
}

type SortHeadBlocksOutput struct {
	title          string
	metaHeadBlocks *[]*HeadBlock
	restHeadBlocks *[]*HeadBlock
}

type SSRInnerHTMLInput struct {
	HwyPrefix                   string
	IsDev                       bool
	BuildID                     string
	LoadersData                 *[]interface{}
	ImportURLs                  *[]string
	OutermostErrorBoundaryIndex int
	SplatSegments               *[]string
	Params                      *map[string]string
	ActionData                  *[]interface{}
	AdHocData                   interface{}
}

func getInitialMatchingPaths(pathToUse string) *[]MatchingPath {
	var initialMatchingPaths []MatchingPath
	for _, path := range *instancePaths {
		var pathType string
		if path.Pattern == "/"+SplatSegment {
			pathType = PathTypeUltimateCatch
		} else {
			pathType = path.PathType
		}
		matcherOutput := matcher(path.Pattern, pathToUse)
		if matcherOutput.matches {
			initialMatchingPaths = append(initialMatchingPaths, MatchingPath{
				Score:              matcherOutput.score,
				RealSegmentsLength: matcherOutput.realSegmentsLength,
				PathType:           pathType,
				OutPath:            path.OutPath,
				Segments:           path.Segments,
				DataFuncs:          path.DataFuncs,
				Params:             matcherOutput.params,
			})
		}
	}
	return &initialMatchingPaths
}

func decoratePaths(paths *[]*MatchingPath) *[]*DecoratedPath {
	decoratedPaths := make([]*DecoratedPath, 0, len(*paths))
	for _, path := range *paths {
		decoratedPaths = append(decoratedPaths, &DecoratedPath{
			DataFuncs: path.DataFuncs,
			PathType:  path.PathType, // only needed for testing __TODO do something smarter here
		})
	}
	return &decoratedPaths
}

func getMatchStrength(pattern string, path string) MatchStrength {
	var patternSegments []string
	for _, segment := range strings.Split(pattern, "/") {
		if len(segment) > 0 {
			patternSegments = append(patternSegments, segment)
		}
	}
	var realSegments []string
	for _, segment := range strings.Split(path, "/") {
		if len(segment) > 0 {
			realSegments = append(realSegments, segment)
		}
	}
	score := 0
	for i := 0; i < len(patternSegments); i++ {
		if len(realSegments) >= len(patternSegments) && patternSegments[i] == realSegments[i] {
			score += 3
			continue
		}
		if patternSegments[i] == SplatSegment {
			score += 1
			continue
		}
		if strings.HasPrefix(patternSegments[i], ":") {
			score += 2
			continue
		}
		break
	}
	return MatchStrength{score, len(realSegments)}
}

func getMatchingPathsInternal(pathsArg *[]MatchingPath, realPath string) (*[]string, *[]*MatchingPath) {
	var paths []*MatchingPath
	for _, x := range *pathsArg {
		// if it's dash route (home), no need to compare segments length
		if x.RealSegmentsLength == 0 {
			paths = append(paths, &x)
			continue
		}

		var indexAdjustedRealSegmentsLength int
		if x.PathType == PathTypeIndex {
			indexAdjustedRealSegmentsLength = x.RealSegmentsLength + 1
		} else {
			indexAdjustedRealSegmentsLength = x.RealSegmentsLength
		}

		// make sure any remaining matches are not longer than the path itself
		shouldMoveOn := len(*x.Segments) <= indexAdjustedRealSegmentsLength
		if !shouldMoveOn {
			continue
		}

		// now we need to remove ineligible indices
		if x.PathType != PathTypeIndex {
			// if not an index, then you're already confirmed good
			paths = append(paths, &x)
			continue
		}

		var truthySegments []string
		for _, segment := range *x.Segments {
			if len(segment) > 0 {
				truthySegments = append(truthySegments, segment)
			}
		}
		var pathSegments []string
		for _, segment := range strings.Split(realPath, "/") {
			if len(segment) > 0 {
				pathSegments = append(pathSegments, segment)
			}
		}
		if len(truthySegments) == len(pathSegments) {
			paths = append(paths, &x)
		}
	}

	// if there are multiple matches, filter out the ultimate catch-all
	if len(paths) > 1 {
		var nonUltimateCatchPaths []*MatchingPath
		for _, x := range paths {
			if x.PathType != PathTypeUltimateCatch {
				nonUltimateCatchPaths = append(nonUltimateCatchPaths, x)
			}
		}
		paths = nonUltimateCatchPaths
	}

	var splatSegments *[]string

	// if only one match now, return it
	if len(paths) == 1 {
		if (paths)[0].PathType == PathTypeUltimateCatch {
			splatSegments = getBaseSplatSegments(realPath)
		}
		return splatSegments, &paths
	}

	// now we only have real child paths

	// these are essentially any matching static layout routes
	var definiteMatches []*MatchingPath // static layout matches
	for _, x := range paths {
		if x.PathType == PathTypeStaticLayout {
			definiteMatches = append(definiteMatches, x)
		}
	}

	highestScoresBySegmentLengthOfDefiniteMatches := getHighestScoresBySegmentLength(&definiteMatches)

	// the "maybe matches" need to compete with each other
	// they also need some more complicated logic

	groupedBySegmentLength := make(GroupedBySegmentLength)

	for _, x := range paths {
		if x.PathType != PathTypeStaticLayout {
			segmentLength := len(*x.Segments)

			highestScoreForThisSegmentLength, exists := highestScoresBySegmentLengthOfDefiniteMatches[segmentLength]

			if !exists || x.Score > highestScoreForThisSegmentLength {
				if groupedBySegmentLength[segmentLength] == nil {
					groupedBySegmentLength[segmentLength] = &[]*MatchingPath{}
				}
				*groupedBySegmentLength[segmentLength] = append(*groupedBySegmentLength[segmentLength], x)
			}
		}
	}

	sortedGroupedBySegmentLength := getSortedGroupedBySegmentLength(groupedBySegmentLength)

	var xformedMaybes []*MatchingPath
	var wildcardSplat *MatchingPath = nil
	for _, paths := range *sortedGroupedBySegmentLength {
		winner := (*paths)[0]
		highestScore := winner.Score
		var indexCandidate *MatchingPath = nil

		for _, path := range *paths {
			if path.PathType == PathTypeIndex && path.RealSegmentsLength < len(*path.Segments) {
				if indexCandidate == nil {
					indexCandidate = path
				} else {
					if path.Score > indexCandidate.Score {
						indexCandidate = path
					}
				}
			}
			if path.Score > highestScore {
				highestScore = path.Score
				winner = path
			}
		}

		if indexCandidate != nil {
			winner = indexCandidate
		}

		// find non ultimate splat
		splat := findNonUltimateSplat(paths)

		if splat != nil {
			if wildcardSplat == nil || splat.Score > wildcardSplat.Score {
				wildcardSplat = splat
			}

			splatSegments = getSplatSegmentsFromWinningPath(winner, realPath)
		}

		// ok, problem
		// in the situation where we have a dynamic folder name with an index file within,
		// we need to make sure that other static-layout paths win over it
		// that's what this code is for

		winnerIsDynamicIndex := getWinnerIsDynamicIndex(winner)

		definiteMatchesShouldOverride := false
		if winnerIsDynamicIndex {
			for _, x := range definiteMatches {
				a := x.PathType == PathTypeStaticLayout
				b := x.RealSegmentsLength == winner.RealSegmentsLength
				var c bool
				if len(*x.Segments) >= 1 && len(*winner.Segments) >= 2 {
					lastSegmentOfX := (*x.Segments)[len(*x.Segments)-1]
					secondToLastSegmentOfWinner := (*winner.Segments)[len(*winner.Segments)-2]
					c = lastSegmentOfX != secondToLastSegmentOfWinner
				}
				d := x.Score > winner.Score
				if a && b && c && d {
					definiteMatchesShouldOverride = true
					break
				}
			}
		}

		if !definiteMatchesShouldOverride {
			xformedMaybes = append(xformedMaybes, winner)
		}
	}

	maybeFinalPaths := getMaybeFinalPaths(&definiteMatches, &xformedMaybes)

	if len(*maybeFinalPaths) > 0 {
		lastPath := (*maybeFinalPaths)[len(*maybeFinalPaths)-1]

		// get index-adjusted segments length
		var lastPathSegmentsLengthConstructive int
		if lastPath.PathType == PathTypeIndex {
			lastPathSegmentsLengthConstructive = len(*lastPath.Segments) - 1
		} else {
			lastPathSegmentsLengthConstructive = len(*lastPath.Segments)
		}

		splatIsTooFarOut := lastPathSegmentsLengthConstructive > lastPath.RealSegmentsLength
		splatIsNeeded := lastPathSegmentsLengthConstructive < lastPath.RealSegmentsLength
		isNotASplat := lastPath.PathType != PathTypeNonUltimateSplat
		weNeedADifferentSplat := splatIsTooFarOut || (splatIsNeeded && isNotASplat)

		if weNeedADifferentSplat {
			if wildcardSplat != nil {
				(*maybeFinalPaths)[len(*maybeFinalPaths)-1] = wildcardSplat
				splatSegments = getSplatSegmentsFromWinningPath(wildcardSplat, realPath)
			} else {
				splatSegments = getBaseSplatSegments(realPath)
				var filteredPaths []*MatchingPath
				for _, x := range *pathsArg {
					if x.PathType == PathTypeUltimateCatch {
						filteredPaths = append(filteredPaths, &x)
						break
					}
				}
				return splatSegments, &filteredPaths
			}
		}
	}

	// if a dynamic layout is adjacent and before an index, we need to remove it
	// IF the index does not share the same dynamic segment
	for i := 0; i < len(*maybeFinalPaths); i++ {
		current := (*maybeFinalPaths)[i]
		var next MatchingPath
		if i+1 < len(*maybeFinalPaths) {
			locNext := (*maybeFinalPaths)[i+1]
			next = *locNext
		}

		if current.PathType == PathTypeDynamicLayout && next.PathType == PathTypeIndex {
			currentDynamicSegment := (*current.Segments)[len(*current.Segments)-1]
			nextDynamicSegment := (*next.Segments)[len(*next.Segments)-2]
			if currentDynamicSegment != nextDynamicSegment {
				*maybeFinalPaths = append((*maybeFinalPaths)[:i], (*maybeFinalPaths)[i+1:]...)
			}
		}
	}

	return splatSegments, maybeFinalPaths
}

func findNonUltimateSplat(paths *[]*MatchingPath) *MatchingPath {
	for _, path := range *paths {
		if path.PathType == PathTypeNonUltimateSplat {
			return path // Return a pointer to the matching path
		}
	}
	return nil // Return nil if no matching path is found
}

func getSortedGroupedBySegmentLength(groupedBySegmentLength GroupedBySegmentLength) *[]*[]*MatchingPath {
	keys := make([]int, 0, len(groupedBySegmentLength))
	for k := range groupedBySegmentLength {
		keys = append(keys, k)
	}

	// Sort the keys in ascending order
	sort.Ints(keys)

	sortedGroupedBySegmentLength := make([]*[]*MatchingPath, 0, len(groupedBySegmentLength))
	for _, k := range keys {
		sortedGroupedBySegmentLength = append(sortedGroupedBySegmentLength, groupedBySegmentLength[k])
	}

	return &sortedGroupedBySegmentLength
}

func getHighestScoresBySegmentLength(matches *[]*MatchingPath) map[int]int {
	highestScores := make(map[int]int)
	for _, match := range *matches {
		segmentLength := len(*match.Segments)
		if currentScore, exists := highestScores[segmentLength]; !exists || match.Score > currentScore {
			highestScores[segmentLength] = match.Score
		}
	}
	return highestScores
}

func getSplatSegmentsFromWinningPath(winner *MatchingPath, realPath string) *[]string {
	data := strings.Split(realPath, "/")

	filteredData := []string{}
	for _, segment := range data {
		if segment != "" {
			filteredData = append(filteredData, segment)
		}
	}

	numOfNonSplatSegments := 0
	for _, x := range *winner.Segments {
		if x != SplatSegment {
			numOfNonSplatSegments++
		}
	}

	numOfSplatSegments := len(filteredData) - numOfNonSplatSegments
	if numOfSplatSegments > 0 {
		final := filteredData[len(filteredData)-numOfSplatSegments:]
		return &final
	} else {
		return &[]string{}
	}
}

func getWinnerIsDynamicIndex(winner *MatchingPath) bool {
	segmentsLength := len(*winner.Segments)
	if winner.PathType == PathTypeIndex && segmentsLength >= 2 {
		secondToLastSegment := (*winner.Segments)[segmentsLength-2]
		return strings.HasPrefix(secondToLastSegment, ":")
	}
	return false
}

func getMaybeFinalPaths(definiteMatches, xformedMaybes *[]*MatchingPath) *[]*MatchingPath {
	maybeFinalPaths := append(*definiteMatches, *xformedMaybes...)
	sort.Slice(maybeFinalPaths, func(i, j int) bool {
		return len(*maybeFinalPaths[i].Segments) < len(*maybeFinalPaths[j].Segments)
	})
	return &maybeFinalPaths
}

func getBaseSplatSegments(realPath string) *[]string {
	var splatSegments []string
	for _, segment := range strings.Split(realPath, "/") {
		if len(segment) > 0 {
			splatSegments = append(splatSegments, segment)
		}
	}
	return &splatSegments
}

var gmpdCache = NewLRUCache(500_000)

func getMatchingPathData(r *http.Request) *ActivePathData {
	realPath := r.URL.Path
	if realPath != "/" && realPath[len(realPath)-1] == '/' {
		realPath = realPath[:len(realPath)-1]
	}

	cached, ok := gmpdCache.Get(realPath)
	item := &gmpdItem{}
	if ok {
		item = cached.(*gmpdItem)
	} else {
		initialMatchingPaths := getInitialMatchingPaths(realPath)
		splatSegments, matchingPaths := getMatchingPathsInternal(initialMatchingPaths, realPath)
		importURLs := make([]string, 0, len(*matchingPaths))
		item.ImportURLs = &importURLs
		for _, path := range *matchingPaths {
			importURLs = append(importURLs, "/"+path.OutPath)
		}
		var lastPath = &MatchingPath{}
		if len(*matchingPaths) > 0 {
			lastPath = (*matchingPaths)[len(*matchingPaths)-1]
		}
		item.FullyDecoratedMatchingPaths = decoratePaths(matchingPaths)
		item.SplatSegments = splatSegments
		item.Params = lastPath.Params
		isSpam := len(*matchingPaths) == 0
		gmpdCache.Set(realPath, item, isSpam)
	}

	var lastPath = &DecoratedPath{}
	if len(*item.FullyDecoratedMatchingPaths) > 0 {
		lastPath = (*item.FullyDecoratedMatchingPaths)[len(*item.FullyDecoratedMatchingPaths)-1]
	}

	var actionData interface{}
	var actionDataError error
	actionExists := lastPath.DataFuncs != nil && lastPath.DataFuncs.Action != nil
	if actionExists {
		actionData, actionDataError = getActionData(r, &lastPath.DataFuncs.Action, item.Params, item.SplatSegments)
	}

	loadersData := make([]interface{}, len(*item.FullyDecoratedMatchingPaths))
	errors := make([]error, len(*item.FullyDecoratedMatchingPaths))
	var wg sync.WaitGroup
	for i, path := range *item.FullyDecoratedMatchingPaths {
		wg.Add(1)
		go func(i int, dataFuncs *DataFuncs) {
			defer wg.Done()
			if dataFuncs == nil || dataFuncs.Loader == nil {
				loadersData[i], errors[i] = nil, nil
				return
			}
			loadersData[i], errors[i] = (dataFuncs.Loader)(DataProps{
				Request:       r,
				Params:        item.Params,
				SplatSegments: item.SplatSegments,
			})
		}(i, path.DataFuncs)
	}
	wg.Wait()

	var thereAreErrors bool
	outermostErrorIndex := -1
	for i, err := range errors {
		if err != nil {
			thereAreErrors = true
			outermostErrorIndex = i
			break
		}
	}

	if actionDataError != nil {
		thereAreErrors = true // __TODO -- test this
		actionDataErrorIndex := len(loadersData) - 1
		if thereAreErrors && actionDataErrorIndex < outermostErrorIndex {
			outermostErrorIndex = actionDataErrorIndex
		}
	}

	closestParentErrorBoundaryIndex := -2

	if thereAreErrors && outermostErrorIndex != -1 {
		closestParentErrorBoundaryIndex = findClosestParentErrorBoundaryIndex(loadersData, outermostErrorIndex)

		if closestParentErrorBoundaryIndex != -1 {
			closestParentErrorBoundaryIndex = outermostErrorIndex - closestParentErrorBoundaryIndex
		}
	}

	var activeHeads []Head
	for _, path := range *item.FullyDecoratedMatchingPaths {
		if path.DataFuncs == nil || path.DataFuncs.Head == nil {
			activeHeads = append(activeHeads, nil)
		} else {
			activeHeads = append(activeHeads, path.DataFuncs.Head)
		}
	}

	// __TODO -- this is a bit of a mess, also should dedupe
	if thereAreErrors {
		var activePathData ActivePathData = ActivePathData{}
		locMatchingPaths := (*item.FullyDecoratedMatchingPaths)[:outermostErrorIndex+1]
		activePathData.MatchingPaths = &locMatchingPaths
		locActiveHeads := activeHeads[:outermostErrorIndex]
		activePathData.ActiveHeads = &locActiveHeads
		locLoadersData := loadersData[:outermostErrorIndex]
		activePathData.LoadersData = &locLoadersData
		locImportURLs := (*item.ImportURLs)[:outermostErrorIndex+1]
		activePathData.ImportURLs = &locImportURLs
		activePathData.OutermostErrorBoundaryIndex = closestParentErrorBoundaryIndex
		*activePathData.ActionData = make([]interface{}, len(*activePathData.ImportURLs))
		activePathData.SplatSegments = item.SplatSegments
		activePathData.Params = item.Params
		return &activePathData
	}
	var activePathData ActivePathData = ActivePathData{}
	activePathData.MatchingPaths = item.FullyDecoratedMatchingPaths
	activePathData.ActiveHeads = &activeHeads
	activePathData.LoadersData = &loadersData
	activePathData.ImportURLs = item.ImportURLs
	activePathData.OutermostErrorBoundaryIndex = closestParentErrorBoundaryIndex
	locActionData := make([]interface{}, len(*activePathData.ImportURLs))
	if len(locActionData) > 0 {
		locActionData[len(locActionData)-1] = actionData
	}
	activePathData.ActionData = &locActionData
	activePathData.SplatSegments = item.SplatSegments
	activePathData.Params = item.Params
	return &activePathData
}

var acceptedMethods = map[string]int{
	"POST": 0, "PUT": 0, "PATCH": 0, "DELETE": 0,
}

func getActionData(r *http.Request, action *Action, params *map[string]string, splatSegments *[]string) (interface{}, error) {
	if _, ok := acceptedMethods[r.Method]; !ok {
		return nil, errors.New("method not allowed")
	}
	if action == nil {
		return nil, nil
	}
	actionFunc := *action
	return actionFunc(DataProps{
		Request:       r,
		Params:        params,
		SplatSegments: splatSegments,
	})
}

func findClosestParentErrorBoundaryIndex(activeErrorBoundaries []interface{}, outermostErrorIndex int) int {
	for i := outermostErrorIndex; i >= 0; i-- {
		if activeErrorBoundaries[i] != nil {
			return len(activeErrorBoundaries) - 1 - i
		}
	}
	return -1
}

func (h Hwy) addDataFuncsToPaths() {
	for i, path := range *instancePaths {
		if dataFuncs, ok := (h.DataFuncsMap)[path.SrcPath]; ok {
			(*instancePaths)[i].DataFuncs = &dataFuncs
		}
	}
}

func (h Hwy) Initialize() error {
	if h.GetBasePaths != nil {
		paths, err := h.GetBasePaths()
		if err != nil {
			return err
		}
		instancePaths = &paths
		h.addDataFuncsToPaths()
	}
	return nil
}

func (h Hwy) GetRouteData(r *http.Request) (*GetRouteDataOutput, error) {
	activePathData := getMatchingPathData(r)

	headBlocks, err := getExportedHeadBlocks(r, activePathData, &h.DefaultHeadBlocks)
	if err != nil {
		return nil, err
	}
	sorted := sortHeadBlocks(headBlocks)
	buildID := "" // __TODO buildID
	if sorted.metaHeadBlocks == nil {
		sorted.metaHeadBlocks = &[]*HeadBlock{}
	}
	if sorted.restHeadBlocks == nil {
		sorted.restHeadBlocks = &[]*HeadBlock{}
	}
	return &GetRouteDataOutput{
		Title:                       sorted.title,
		MetaHeadBlocks:              sorted.metaHeadBlocks,
		RestHeadBlocks:              sorted.restHeadBlocks,
		LoadersData:                 activePathData.LoadersData,
		ImportURLs:                  activePathData.ImportURLs,
		OutermostErrorBoundaryIndex: activePathData.OutermostErrorBoundaryIndex,
		SplatSegments:               activePathData.SplatSegments,
		Params:                      activePathData.Params,
		ActionData:                  activePathData.ActionData,
		AdHocData:                   nil, // __TODO
		BuildID:                     buildID,
	}, nil
}

func getExportedHeadBlocks(r *http.Request, activePathData *ActivePathData, defaultHeadBlocks *[]HeadBlock) (*[]*HeadBlock, error) {
	headBlocks := make([]HeadBlock, len(*defaultHeadBlocks))
	copy(headBlocks, *defaultHeadBlocks)
	for i, head := range *activePathData.ActiveHeads {
		if head != nil {
			headProps := HeadProps{
				DataProps: DataProps{
					Request:       r,
					Params:        activePathData.Params,
					SplatSegments: activePathData.SplatSegments,
				},
				LoaderData: (*activePathData.LoadersData)[i],
				ActionData: (*activePathData.ActionData)[i],
			}
			localHeadBlocks, err := (head)(headProps)
			if err != nil {
				return nil, err
			}
			headBlocks = append(headBlocks, *localHeadBlocks...)
		}
	}
	return dedupeHeadBlocks(&headBlocks), nil
}

// __TODO -- add OverrideMatchingParentsFunc that acts just like Head but lets you return simpler HeadBlocks that when matched, override the parent HeadBlocks
// additionally, would make sense to also take an a defaultOverrideHeadBlocks arg at root as well, just like DefaultHeadBlocks
// ALternatively, could build the concept into each Path level as a new opportunity to set a DefaultHeadBlocks slice, applicable to it and its children

// __TODO test this
func dedupeHeadBlocks(blocks *[]HeadBlock) *[]*HeadBlock {
	uniqueBlocks := make(map[string]*HeadBlock)
	var dedupedBlocks []*HeadBlock

	titleIdx := -1
	descriptionIdx := -1

	for _, block := range *blocks {
		if title := (block.Title); len(title) > 0 {
			if titleIdx == -1 {
				titleIdx = len(dedupedBlocks)
				dedupedBlocks = append(dedupedBlocks, &block)
			} else {
				dedupedBlocks[titleIdx] = &block
			}
		} else if block.Tag == "meta" && (block.Attributes)["name"] == "description" {
			if descriptionIdx == -1 {
				descriptionIdx = len(dedupedBlocks)
				dedupedBlocks = append(dedupedBlocks, &block)
			} else {
				dedupedBlocks[descriptionIdx] = &block
			}
		} else {
			key := stableHash(&block)
			if _, exists := uniqueBlocks[key]; !exists {
				uniqueBlocks[key] = &block
				dedupedBlocks = append(dedupedBlocks, &block)
			}
		}
	}

	return &dedupedBlocks
}

func stableHash(block *HeadBlock) string {
	parts := make([]string, 0, len(block.Attributes))
	for key, value := range block.Attributes {
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts) // Ensure attributes are in a consistent order
	var sb strings.Builder
	sb.Grow(len(block.Tag) + 1 + (len(parts) * 16))
	sb.WriteString(block.Tag)
	sb.WriteString("|")
	for i, part := range parts {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(part)
	}
	return sb.String()
}

func sortHeadBlocks(blocks *[]*HeadBlock) SortHeadBlocksOutput {
	result := SortHeadBlocksOutput{}
	result.metaHeadBlocks = &[]*HeadBlock{}
	result.restHeadBlocks = &[]*HeadBlock{}
	for _, block := range *blocks {
		if len(block.Title) > 0 {
			result.title = block.Title
		} else if block.Tag == "meta" {
			*result.metaHeadBlocks = append(*result.metaHeadBlocks, block)
		} else {
			*result.restHeadBlocks = append(*result.restHeadBlocks, block)
		}
	}
	return result
}

var metaStart = HeadBlock{Tag: "meta", Attributes: map[string]string{"data-hwy": "meta-start"}}
var metaEnd = HeadBlock{Tag: "meta", Attributes: map[string]string{"data-hwy": "meta-end"}}
var restStart = HeadBlock{Tag: "meta", Attributes: map[string]string{"data-hwy": "rest-start"}}
var restEnd = HeadBlock{Tag: "meta", Attributes: map[string]string{"data-hwy": "rest-end"}}

func GetHeadElements(routeData *GetRouteDataOutput) (*template.HTML, error) {
	var htmlBuilder strings.Builder
	titleTmpl, err := template.New("title").Parse(
		`<title>{{.}}</title>` + "\n",
	)
	if err != nil {
		return nil, err
	}
	err = titleTmpl.Execute(&htmlBuilder, routeData.Title)
	if err != nil {
		return nil, err
	}

	var headBlocks = []*HeadBlock{&metaStart}
	headBlocks = append(headBlocks, append(*routeData.MetaHeadBlocks, &metaEnd)...)
	headBlocks = append(headBlocks, &restStart)
	headBlocks = append(headBlocks, append(*routeData.RestHeadBlocks, &restEnd)...)

	headElsTmpl, err := template.New("headblock").Parse(
		`{{range $key, $value := .Attributes}}{{$key}}="{{$value}}" {{end}} />` + "\n",
	)
	if err != nil {
		return nil, err
	}
	for _, block := range headBlocks {
		if !slices.Contains(permittedTags, block.Tag) {
			continue
		}
		htmlBuilder.WriteString("<" + block.Tag + " ")
		err = headElsTmpl.Execute(&htmlBuilder, block)
		if err != nil {
			return nil, err
		}
	}
	final := template.HTML(htmlBuilder.String())
	return &final, nil
}

var permittedTags = []string{"meta", "base", "link", "style", "script", "noscript"}

const HwyPrefix = "__hwy_internal__"

func GetSSRInnerHTML(routeData *GetRouteDataOutput, isDev bool) (*template.HTML, error) {
	tmpl, err := template.New("ssr").Parse(`<script>
	globalThis[Symbol.for("{{.HwyPrefix}}")] = {};
	const x = globalThis[Symbol.for("{{.HwyPrefix}}")];
	x.isDev = {{.IsDev}};
	x.buildID = {{.BuildID}};
	x.loadersData = {{.LoadersData}};
	x.importURLs = {{.ImportURLs}};
	x.outermostErrorBoundaryIndex = {{.OutermostErrorBoundaryIndex}};
	x.splatSegments = {{.SplatSegments}};
	x.params = {{.Params}};
	x.actionData = {{.ActionData}};
	x.adHocData = {{.AdHocData}};
</script>`)
	if err != nil {
		return nil, err
	}
	var htmlBuilder strings.Builder
	var dto = SSRInnerHTMLInput{
		HwyPrefix:                   HwyPrefix,
		IsDev:                       isDev,
		BuildID:                     routeData.BuildID,
		LoadersData:                 routeData.LoadersData,
		ImportURLs:                  routeData.ImportURLs,
		OutermostErrorBoundaryIndex: routeData.OutermostErrorBoundaryIndex,
		SplatSegments:               routeData.SplatSegments,
		Params:                      routeData.Params,
		ActionData:                  routeData.ActionData,
		AdHocData:                   routeData.AdHocData,
	}
	err = tmpl.Execute(&htmlBuilder, dto)
	if err != nil {
		return nil, err
	}
	final := template.HTML(htmlBuilder.String())
	return &final, nil
}

func GetIsJSONRequest(r *http.Request) bool {
	queryKey := HwyPrefix + "json"
	return r.URL.Query().Get(queryKey) != ""
}

func matcher(pattern string, path string) matcherOutput {
	pattern = strings.TrimPrefix(pattern, "/")
	path = strings.TrimPrefix(path, "/")
	patternSegments := strings.Split(pattern, "/")
	pathSegments := strings.Split(path, "/")
	adjPatternSegmentsLength := len(patternSegments)
	pathSegmentsLength := len(pathSegments)
	isCatch := patternSegments[adjPatternSegmentsLength-1] == SplatSegment
	if isCatch {
		adjPatternSegmentsLength--
	}
	if adjPatternSegmentsLength > pathSegmentsLength {
		return matcherOutput{}
	}
	matches := false
	params := make(map[string]string)
	if pattern == path {
		matches = true
	} else {
		for i, patternSegment := range patternSegments {
			if i < pathSegmentsLength && patternSegment == pathSegments[i] {
				matches = true
				continue
			}
			if patternSegment == SplatSegment {
				matches = true
				continue
			}
			if strings.HasPrefix(patternSegment, ":") {
				matches = true
				paramKey := patternSegment[1:]
				if paramKey != "catch*" {
					params[paramKey] = pathSegments[i]
				}
				continue
			}
			matches = false
			break
		}
	}
	if !matches {
		return matcherOutput{}
	}
	strength := getMatchStrength(pattern, path)
	return matcherOutput{
		path:               path,
		pattern:            pattern,
		matches:            matches,
		params:             &params,
		score:              strength.Score,
		realSegmentsLength: strength.RealSegmentsLength,
	}
}
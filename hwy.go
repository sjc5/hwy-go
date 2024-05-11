package hwy

import router "github.com/sjc5/hwy-go/router"

type BuildOptions = router.BuildOptions
type Hwy = router.Hwy
type HeadBlock = router.HeadBlock
type DataFuncsMap = router.DataFuncsMap
type LoaderProps = router.LoaderProps
type ActionProps = router.ActionProps
type HeadProps = router.HeadProps
type Path = router.Path
type PathsFile = router.PathsFile
type Loader = router.Loader
type Action = router.Action
type Head = router.Head

var Build = router.Build
var GenerateTS = router.GenerateTS
var NewLRUCache = router.NewLRUCache
var GetIsJSONRequest = router.GetIsJSONRequest
var GetHeadElements = router.GetHeadElements
var GetSSRInnerHTML = router.GetSSRInnerHTML

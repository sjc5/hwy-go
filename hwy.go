package hwy

import router "github.com/sjc5/hwy-go/router"

type BuildOptions = router.BuildOptions
type Hwy = router.Hwy
type HeadBlock = router.HeadBlock
type DataFuncsMap = router.DataFuncsMap
type DataProps = router.DataProps
type HeadProps = router.HeadProps
type Path = router.Path
type PathsFile = router.PathsFile

var Build = router.Build
var NewLRUCache = router.NewLRUCache
var GetIsJSONRequest = router.GetIsJSONRequest
var GetHeadElements = router.GetHeadElements
var GetSSRInnerHTML = router.GetSSRInnerHTML

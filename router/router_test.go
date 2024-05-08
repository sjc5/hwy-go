package router

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

type expectedOutput struct {
	MatchingPaths []string
	Params        map[string]string
	SplatSegments []string
}

type testPath struct {
	Path           string
	ExpectedOutput struct {
		MatchingPaths []string
		Params        map[string]string
		SplatSegments []string
	}
}

var testPaths = []testPath{
	{
		Path: "/does-not-exist",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeUltimateCatch},
			SplatSegments: []string{"does-not-exist"},
		},
	},
	{
		Path: "/this-should-be-ignored",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeUltimateCatch},
			SplatSegments: []string{"this-should-be-ignored"},
		},
	},
	{
		Path: "/",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeIndex},
		},
	},
	{
		Path: "/lion",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeIndex},
		},
	},
	{
		Path: "/lion/123",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeNonUltimateSplat},
			SplatSegments: []string{"123"},
		},
	},
	{
		Path: "/lion/123/456",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeNonUltimateSplat},
			SplatSegments: []string{"123", "456"},
		},
	},
	{
		Path: "/lion/123/456/789",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeNonUltimateSplat},
			SplatSegments: []string{"123", "456", "789"},
		},
	},
	{
		Path: "/tiger",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeIndex},
		},
	},
	{
		Path: "/tiger/123",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeIndex},
			Params:        map[string]string{"tiger_id": "123"},
		},
	},
	{
		Path: "/tiger/123/456",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeDynamicLayout},
			Params:        map[string]string{"tiger_id": "123", "tiger_cub_id": "456"},
		},
	},
	{
		Path: "/tiger/123/456/789",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeNonUltimateSplat},
			Params:        map[string]string{"tiger_id": "123"},
			SplatSegments: []string{"456", "789"},
		},
	},
	{
		Path: "/bear",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeIndex},
		},
	},
	{
		Path: "/bear/123",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout},
			Params:        map[string]string{"bear_id": "123"},
		},
	},
	{
		Path: "/bear/123/456",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeNonUltimateSplat},
			Params:        map[string]string{"bear_id": "123"},
			SplatSegments: []string{"456"},
		},
	},
	{
		Path: "/bear/123/456/789",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeNonUltimateSplat},
			Params:        map[string]string{"bear_id": "123"},
			SplatSegments: []string{"456", "789"},
		},
	},
	{
		Path: "/dashboard",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeIndex},
		},
	},
	{
		Path: "/dashboard/asdf",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeNonUltimateSplat},
			SplatSegments: []string{"asdf"},
		},
	},
	{
		Path: "/dashboard/customers",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeStaticLayout, PathTypeIndex},
		},
	},
	{
		Path: "/dashboard/customers/123",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeIndex},
			Params:        map[string]string{"customer_id": "123"},
		},
	},
	{
		Path: "/dashboard/customers/123/orders",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeStaticLayout, PathTypeIndex},
			Params:        map[string]string{"customer_id": "123"},
		},
	},
	{
		Path: "/dashboard/customers/123/orders/456",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout, PathTypeStaticLayout, PathTypeDynamicLayout, PathTypeStaticLayout, PathTypeDynamicLayout},
			Params:        map[string]string{"customer_id": "123", "order_id": "456"},
		},
	},
	{
		Path: "/articles",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeIndex},
		},
	},
	{
		Path: "/articles/bob",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeUltimateCatch},
			SplatSegments: []string{"articles", "bob"},
		},
	},
	{
		Path: "/articles/test",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeUltimateCatch},
			SplatSegments: []string{"articles", "test"},
		},
	},
	{
		Path: "/articles/test/articles",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeIndex},
		},
	},
	{
		Path: "/dynamic-index/index",
		ExpectedOutput: expectedOutput{
			MatchingPaths: []string{PathTypeStaticLayout},
		},
	},
}

func TestRouter(t *testing.T) {
	for _, path := range testPaths {
		matchingPathData := test(path.Path)
		if len(*matchingPathData.MatchingPaths) != len(path.ExpectedOutput.MatchingPaths) {
			fmt.Println("Path:", path.Path)
			t.Errorf("Expected %d matching paths, but got %d", len(path.ExpectedOutput.MatchingPaths), len(*matchingPathData.MatchingPaths))
		}
		for i, matchingPath := range *matchingPathData.MatchingPaths {
			if matchingPath.PathType != path.ExpectedOutput.MatchingPaths[i] {
				fmt.Println("Path:", path.Path)
				t.Errorf("Expected matching path %d to be of type %s, but got %s", i, path.ExpectedOutput.MatchingPaths[i], matchingPath.PathType)
			}
		}
		if len(*matchingPathData.Params) != len(path.ExpectedOutput.Params) {
			fmt.Println("Path:", path.Path)
			t.Errorf("Expected %d params, but got %d", len(path.ExpectedOutput.Params), len(*matchingPathData.Params))
		}
		for key, value := range path.ExpectedOutput.Params {
			if (*matchingPathData.Params)[key] != value {
				fmt.Println("Path:", path.Path)
				t.Errorf("Expected param %s to be %s, but got %s", key, value, (*matchingPathData.Params)[key])
			}
		}
		if matchingPathData.SplatSegments != nil && len(*matchingPathData.SplatSegments) != len(path.ExpectedOutput.SplatSegments) {
			fmt.Println("Path:", path.Path)
			t.Errorf("Expected %d splat segments, but got %d", len(path.ExpectedOutput.SplatSegments), len(*matchingPathData.SplatSegments))
		}
		for i, splatSegment := range path.ExpectedOutput.SplatSegments {
			if (*matchingPathData.SplatSegments)[i] != splatSegment {
				fmt.Println("Path:", path.Path)
				t.Errorf("Expected splat segment %d to be %s, but got %s", i, splatSegment, (*matchingPathData.SplatSegments)[i])
			}
		}
	}
}

func test(path string) *ActivePathData {
	var r http.Request = http.Request{}
	r.URL = &url.URL{}
	r.URL.Path = path
	r.Method = "GET"
	instancePaths = &dummyPaths
	return getMatchingPathData(nil, &r)
}

var dummyPaths = []Path{
	{
		Pattern:  "/$",
		Segments: &[]string{"$"},
		PathType: "ultimate-catch",
	},
	{
		Pattern:  "/_index",
		Segments: &[]string{""},
		PathType: "index",
	},
	{
		Pattern:  "/bear",
		Segments: &[]string{"bear"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/dashboard",
		Segments: &[]string{"dashboard"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/lion",
		Segments: &[]string{"lion"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/tiger",
		Segments: &[]string{"tiger"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/tiger/$tiger_id",
		Segments: &[]string{"tiger", "$tiger_id"},
		PathType: "dynamic-layout",
	},
	{
		Pattern:  "/tiger/_index",
		Segments: &[]string{"tiger", ""},
		PathType: "index",
	},
	{
		Pattern:  "/tiger/$tiger_id/$",
		Segments: &[]string{"tiger", "$tiger_id", "$"},
		PathType: "non-ultimate-splat",
	},
	{
		Pattern:  "/tiger/$tiger_id/$tiger_cub_id",
		Segments: &[]string{"tiger", "$tiger_id", "$tiger_cub_id"},
		PathType: "dynamic-layout",
	},
	{
		Pattern:  "/tiger/$tiger_id/_index",
		Segments: &[]string{"tiger", "$tiger_id", ""},
		PathType: "index",
	},
	{
		Pattern:  "/lion/$",
		Segments: &[]string{"lion", "$"},
		PathType: "non-ultimate-splat",
	},
	{
		Pattern:  "/lion/_index",
		Segments: &[]string{"lion", ""},
		PathType: "index",
	},
	{
		Pattern:  "/dynamic-index/index",
		Segments: &[]string{"dynamic-index", "index"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/dynamic-index/$pagename/_index",
		Segments: &[]string{"dynamic-index", "$pagename", ""},
		PathType: "index",
	},
	{
		Pattern:  "/dashboard/$",
		Segments: &[]string{"dashboard", "$"},
		PathType: "non-ultimate-splat",
	},
	{
		Pattern:  "/dashboard/_index",
		Segments: &[]string{"dashboard", ""},
		PathType: "index",
	},
	{
		Pattern:  "/dashboard/customers",
		Segments: &[]string{"dashboard", "customers"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/dashboard/customers/$customer_id",
		Segments: &[]string{"dashboard", "customers", "$customer_id"},
		PathType: "dynamic-layout",
	},
	{
		Pattern:  "/dashboard/customers/_index",
		Segments: &[]string{"dashboard", "customers", ""},
		PathType: "index",
	},
	{
		Pattern:  "/dashboard/customers/$customer_id/_index",
		Segments: &[]string{"dashboard", "customers", "$customer_id", ""},
		PathType: "index",
	},
	{
		Pattern:  "/dashboard/customers/$customer_id/orders",
		Segments: &[]string{"dashboard", "customers", "$customer_id", "orders"},
		PathType: "static-layout",
	},
	{
		Pattern:  "/dashboard/customers/$customer_id/orders/$order_id",
		Segments: &[]string{"dashboard", "customers", "$customer_id", "orders", "$order_id"},
		PathType: "dynamic-layout",
	},
	{
		Pattern:  "/dashboard/customers/$customer_id/orders/_index",
		Segments: &[]string{"dashboard", "customers", "$customer_id", "orders", ""},
		PathType: "index",
	},
	{
		Pattern:  "/bear/$bear_id",
		Segments: &[]string{"bear", "$bear_id"},
		PathType: "dynamic-layout",
	},
	{
		Pattern:  "/bear/_index",
		Segments: &[]string{"bear", ""},
		PathType: "index",
	},
	{
		Pattern:  "/bear/$bear_id/$",
		Segments: &[]string{"bear", "$bear_id", "$"},
		PathType: "non-ultimate-splat",
	},
	{
		Pattern:  "/articles/_index",
		Segments: &[]string{"articles", ""},
		PathType: "index",
	},
	{
		Pattern:  "/articles/test/articles/_index",
		Segments: &[]string{"articles", "test", "articles", ""},
		PathType: "index",
	},
}

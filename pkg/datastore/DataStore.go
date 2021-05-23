//go:generate protoc --go_out=module=github.com/ferocious-space/evesso/datastore:. proto/*.proto
//go:generate protoc-go-inject-tag -input=./AccountData.pb.go

package datastore

import (
	"context"
)

type DataStore interface {
	ProfileStore
}

type ProfileStore interface {
	NewProfile(ctx context.Context, profileName string, data interface{}) (*Profile, error)
	GetProfile(ctx context.Context, profileID string) (*Profile, error)
	FindProfile(ctx context.Context, profileName string) (*Profile, error)
	DeleteProfile(ctx context.Context, profileID string) error

	GetPKCE(ctx context.Context, state string) (*PKCE, error)
	CleanPKCE(ctx context.Context) error
}

func MatchScopes(x, y []string) bool {
	xLen := len(x)
	yLen := len(y)

	if xLen != yLen {
		return false
	}

	if xLen > 20 {
		return elementsMatchByMap(x, y)
	} else {
		return elementsMatchByLoop(x, y)
	}

}

func elementsMatchByLoop(x, y []string) bool {
	xLen := len(x)
	yLen := len(y)

	visited := make([]bool, yLen)

	for i := 0; i < xLen; i++ {
		found := false
		element := x[i]
		for j := 0; j < yLen; j++ {
			if visited[j] {
				continue
			}
			if element == y[j] {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func elementsMatchByMap(x, y []string) bool {
	// create a map of string -> int
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y] -= 1
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	if len(diff) == 0 {
		return true
	}
	return false
}

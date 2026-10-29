module optic

go 1.24.7

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	golang.org/x/sys v0.37.0
)

require github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect

replace (
	golang.org/x/exp => github.com/golang/exp v0.0.0-20230425010034-47ecfdc1ba53
	golang.org/x/sys => github.com/golang/sys v0.37.0
	gopkg.in/Knetic/govaluate.v3 => github.com/Knetic/govaluate v3.0.0+incompatible
)

package unknown

import "net/url"

type Bad struct {
	Where url.URL `json:"where"`
}

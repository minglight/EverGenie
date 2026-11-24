package report

import "net/url"

func urlDecode(s string) (string, error) { return url.QueryUnescape(s) }

package mapping

import (
	"encoding/json"
	"time"
)

type Mode string

const (
	ModeFast Mode = "fast"
	ModeSlow Mode = "slow"
)

type Free string

type Alias = Mode

type Everything struct {
	Name     string                 `json:"name"`
	Opt      string                 `json:"opt,omitempty"`
	Skipped  string                 `json:"-"`
	Flag     bool                   `json:"flag"`
	Count    int32                  `json:"count"`
	Big      int64                  `json:"big"`
	Ratio    float64                `json:"ratio"`
	At       time.Time              `json:"at"`
	Wait     time.Duration          `json:"wait"`
	List     []string               `json:"list"`
	Matrix   [][]int                `json:"matrix"`
	Mapped   map[string]Mode        `json:"mapped"`
	Ptr      *Everything            `json:"ptr"`
	PtrStr   *string                `json:"ptr_str,omitempty"`
	Anything any                    `json:"anything"`
	Legacy   interface{}            `json:"legacy"`
	Raw      json.RawMessage        `json:"raw"`
	Data     []byte                 `json:"data"`
	Kind     Mode                   `json:"kind"`
	Loose    Free                   `json:"loose"`
	Refs     map[string]*Everything `json:"refs,omitempty"`
	Rec      *Record                `json:"rec,omitempty"`
	Dashed   string                 `json:"dashed-key"`
	unexport string
}

type Record struct {
	ID string `json:"id"`
}

type Service interface {
	Do()
}

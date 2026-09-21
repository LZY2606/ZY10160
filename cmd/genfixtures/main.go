// Command genfixtures regenerates the committed acceptance fixtures.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"polaritytrace/internal/fixture"
)

type rawStep struct {
	Key       string     `json:"key"`
	Treatment string     `json:"treatment"`
	Level     float64    `json:"level"`
	Rep       int        `json:"rep"`
	V         [3]float64 `json:"v"`
	Cov       [6]float64 `json:"cov"`
}

func main() {
	outDir := "internal/fixture/data"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	specs := fixture.Generate()
	for _, s := range specs {
		doc := struct {
			ID          string      `json:"id"`
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Mount       interface{} `json:"mount"`
			Bedding     interface{} `json:"bedding"`
			Steps       []rawStep   `json:"steps"`
		}{ID: s.ID, Name: s.Name, Description: s.Description, Mount: s.Mount, Bedding: s.Bedding}
		for _, st := range s.Steps {
			doc.Steps = append(doc.Steps, rawStep{
				Key: st.Key, Treatment: st.Treatment, Level: st.Level, Rep: st.Rep,
				V:   [3]float64{st.X, st.Y, st.Z},
				Cov: st.Cov,
			})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			panic(err)
		}
		path := filepath.Join(outDir, s.ID+".json")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("wrote", path)
	}
}

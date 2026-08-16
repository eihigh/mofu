// Package cubism reads the plain-JSON halves of a Cubism model export:
// model3.json and motion3.json. The binary .moc3 is left to the Core.
package cubism

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Model3 is the contents of a .model3.json file.
type Model3 struct {
	Version        int            `json:"Version"`
	FileReferences FileReferences `json:"FileReferences"`
	Groups         []Group        `json:"Groups"`
	HitAreas       []HitArea      `json:"HitAreas"`

	// Dir is the directory the file was read from; every path in
	// FileReferences is relative to it.
	Dir string `json:"-"`
}

// FileReferences lists the sibling files a model3.json points at.
type FileReferences struct {
	Moc         string                 `json:"Moc"`
	Textures    []string               `json:"Textures"`
	Physics     string                 `json:"Physics"`
	Pose        string                 `json:"Pose"`
	DisplayInfo string                 `json:"DisplayInfo"`
	Expressions []ExpressionRef        `json:"Expressions"`
	Motions     map[string][]MotionRef `json:"Motions"`
	UserData    string                 `json:"UserData"`
}

// ExpressionRef names one .exp3.json file.
type ExpressionRef struct {
	Name string `json:"Name"`
	File string `json:"File"`
}

// MotionRef names one .motion3.json file plus its playback metadata.
type MotionRef struct {
	File        string  `json:"File"`
	Sound       string  `json:"Sound"`
	FadeInTime  float64 `json:"FadeInTime"`
	FadeOutTime float64 `json:"FadeOutTime"`
}

// Group is a named set of parameter or part ids, such as EyeBlink or LipSync.
type Group struct {
	Target string   `json:"Target"`
	Name   string   `json:"Name"`
	Ids    []string `json:"Ids"`
}

// HitArea associates a drawable with a logical hit region name.
type HitArea struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
}

// LoadModel3 reads and parses a .model3.json file.
func LoadModel3(path string) (*Model3, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Model3
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.FileReferences.Moc == "" {
		return nil, fmt.Errorf("%s: no Moc reference", path)
	}
	m.Dir = filepath.Dir(path)
	return &m, nil
}

// Resolve turns a path relative to the model3.json into an absolute-ish path
// usable with os.Open.
func (m *Model3) Resolve(rel string) string {
	if rel == "" {
		return ""
	}
	return filepath.Join(m.Dir, filepath.FromSlash(rel))
}

// MotionGroups returns the motion group names in a stable order.
func (m *Model3) MotionGroups() []string {
	names := make([]string, 0, len(m.FileReferences.Motions))
	for k := range m.FileReferences.Motions {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

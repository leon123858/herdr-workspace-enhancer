package config

import (
	"github.com/spf13/viper"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigRoundTripAndConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := &Store{Path: path}
	if r, e := s.LoadRatio(); e != nil || r != .35 {
		t.Fatal(r, e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("reading created config")
	}
	if e := os.WriteFile(path, []byte("theme = 'dark'\n[tui]\nworkspace_ratio=0.4\nother = true\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := (&Store{Path: path}).SaveRatio(.2 + float64(i)/10); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	r, e := s.LoadRatio()
	if e != nil || r < .2 || r > .7 {
		t.Fatal(r, e)
	}
	v := viper.New()
	v.SetConfigFile(path)
	if e = v.ReadInConfig(); e != nil {
		t.Fatal(e)
	}
	if v.GetString("theme") != "dark" || !v.GetBool("tui.other") {
		t.Fatal("lost unrelated fields")
	}
	if e = s.SaveRatio(.65); e != nil {
		t.Fatal(e)
	}
	if r, e = (&Store{Path: path}).LoadRatio(); e != nil || r != .65 {
		t.Fatal(r, e)
	}
}
func TestInvalidConfigAndWriteFailure(t *testing.T) {
	for _, text := range []string{"[broken", "[tui]\nworkspace_ratio = 'nope'", "[tui]\nworkspace_ratio=0.01", "[tui]\nworkspace_ratio=nan"} {
		t.Run(text, func(t *testing.T) {
			s := &Store{Path: filepath.Join(t.TempDir(), "config.toml")}
			os.WriteFile(s.Path, []byte(text), 0600)
			if r, e := s.LoadRatio(); e == nil || r != .35 {
				t.Fatal(r, e)
			}
			if text == "[broken" {
				if e := s.SaveRatio(.5); e == nil {
					t.Fatal("overwrote corrupt config")
				}
				b, _ := os.ReadFile(s.Path)
				if string(b) != text {
					t.Fatal(string(b))
				}
			}
		})
	}
	s := &Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	for _, r := range []float64{0, 1, math.NaN(), math.Inf(1)} {
		if e := s.SaveRatio(r); e == nil {
			t.Fatal("accepted", r)
		}
	}
	os.WriteFile(s.Path, []byte("other=true"), 0600)
	if r, e := s.LoadRatio(); e != nil || r != .35 {
		t.Fatal(r, e)
	}
	s.Path = filepath.Join(s.Path, "config.toml")
	if e := s.SaveRatio(.5); e == nil {
		t.Fatal("expected invalid directory error")
	}
}

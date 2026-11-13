package usage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func put(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	future, past := now.Unix()+3600, now.Unix()-60
	t.Run("missing dir", func(t *testing.T) {
		s := Load(filepath.Join(t.TempDir(), "none"), now)
		if s.FiveHour.Known || s.SevenDay.Known {
			t.Errorf("want unknown, got %+v", s)
		}
	})
	t.Run("empty dir", func(t *testing.T) {
		if s := Load(t.TempDir(), now); s.FiveHour.Known {
			t.Error("want unknown")
		}
	})
	t.Run("one window missing", func(t *testing.T) {
		d := t.TempDir()
		put(t, d, "a.json", `{"updated":1799999990,"five_hour":{"used_percentage":42,"resets_at":1800003600}}`)
		s := Load(d, now)
		if !s.FiveHour.Known || s.FiveHour.Percent != 42 || s.FiveHour.Age != 10*time.Second {
			t.Errorf("five = %+v", s.FiveHour)
		}
		if s.SevenDay.Known {
			t.Error("seven_day must be unknown, not 0%")
		}
	})
	t.Run("stale window", func(t *testing.T) {
		d := t.TempDir()
		put(t, d, "a.json", `{"updated":1799990000,"five_hour":{"used_percentage":90,"resets_at":`+itoa(past)+`},"seven_day":{"used_percentage":10,"resets_at":`+itoa(future)+`}}`)
		s := Load(d, now)
		if s.FiveHour.Known {
			t.Error("stale five_hour must be unknown")
		}
		if !s.SevenDay.Known {
			t.Error("seven_day should still show")
		}
	})
	t.Run("malformed skipped", func(t *testing.T) {
		d := t.TempDir()
		put(t, d, "bad.json", `{nope`)
		put(t, d, "ok.json", `{"updated":1799999000,"seven_day":{"used_percentage":7,"resets_at":`+itoa(future)+`}}`)
		if s := Load(d, now); !s.SevenDay.Known || s.SevenDay.Percent != 7 {
			t.Errorf("got %+v", s)
		}
	})
	t.Run("freshest wins per window", func(t *testing.T) {
		d := t.TempDir()
		put(t, d, "old.json", `{"updated":100,"five_hour":{"used_percentage":10,"resets_at":`+itoa(future)+`},"seven_day":{"used_percentage":1,"resets_at":`+itoa(future)+`}}`)
		put(t, d, "new.json", `{"updated":200,"five_hour":{"used_percentage":55,"resets_at":`+itoa(future)+`}}`)
		s := Load(d, now)
		if s.FiveHour.Percent != 55 {
			t.Errorf("five = %v, want freshest 55", s.FiveHour.Percent)
		}
		if s.SevenDay.Percent != 1 {
			t.Errorf("seven = %v, want fallback to older file", s.SevenDay.Percent)
		}
	})
	t.Run("fresh stale file loses to older valid one", func(t *testing.T) {
		d := t.TempDir()
		put(t, d, "old.json", `{"updated":100,"five_hour":{"used_percentage":20,"resets_at":`+itoa(future)+`}}`)
		put(t, d, "new.json", `{"updated":200,"five_hour":{"used_percentage":99,"resets_at":`+itoa(past)+`}}`)
		if s := Load(d, now); s.FiveHour.Percent != 20 {
			t.Errorf("got %v", s.FiveHour.Percent)
		}
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

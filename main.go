package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/native"
)

func logPanicsTo() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, "Library", "Logs")
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "CFE.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = syscall.Dup2(int(f.Fd()), 2)
	os.Stderr = f
	log.SetOutput(f)
	log.Printf("---- CFE launched %s ----", time.Now().Format(time.RFC3339))
}

var (
	startDir     string
	customChrome bool
)

func main() {
	nativeFlag := flag.Bool("native", false, "open as a desktop window instead of serving HTTP")
	addr := flag.String("addr", ":8090", `listen address - ":8090" or "unix:/path.sock"`)
	flag.StringVar(&startDir, "root", "", "starting directory (default: your home)")
	shot := flag.String("shot", "", "headless verification: render offscreen to this PNG and exit")
	settle := flag.Float64("settle", 2500, "shot mode: ms to run before capturing")
	clicks := flag.String("clicks", "", `shot mode: scripted input, "x,y@ms;type:txt@ms;key:cmd+g@ms"`)
	flag.Parse()

	caution.ServeClient()

	if *nativeFlag || *shot != "" || native.InBundle() {
		if native.InBundle() {
			logPanicsTo()
		}
		w, h := 1280, 820 // shot mode stays fixed so headless renders are deterministic
		if *shot == "" {
			p := loadPrefs()
			w, h = p.WinW, p.WinH
		}

		customChrome = true
		err := native.Run(mount, native.Options{
			Title: "CFE :: THE GIBSON", W: w, H: h,
			CustomTitlebar: true, TitlebarStyle: "compact",
			Shot: *shot, SettleMs: *settle, Clicks: *clicks,
		})
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("CFE on %s - jack in", *addr)
	log.Fatal(caution.Serve(*addr, mount))
}

// lux is a tiny CLI for the Luxafor Flag USB light.
package main

import (
	"fmt"
	"maps"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sstallion/go-hid"
)

const (
	vendorID  = 0x04d8
	productID = 0xf372

	allLEDs = 0xff
	front   = 0x41 // LEDs 1-3, the flag
	back    = 0x42 // LEDs 4-6, the base
)

type rgb struct{ r, g, b byte }

var (
	off   = rgb{}
	white = rgb{0xff, 0xff, 0xff}
)

// Every channel a colour uses runs flat out at 255. warm is a softer white on purpose.
var colors = map[string]rgb{
	"red":     {0xff, 0x00, 0x00},
	"green":   {0x00, 0xff, 0x00},
	"blue":    {0x00, 0x00, 0xff},
	"white":   white,
	"yellow":  {0xff, 0xff, 0x00},
	"cyan":    {0x00, 0xff, 0xff},
	"magenta": {0xff, 0x00, 0xff},
	"purple":  {0xaa, 0x00, 0xff},
	"pink":    {0xff, 0x00, 0x80},
	"orange":  {0xff, 0x60, 0x00},
	"teal":    {0x00, 0xff, 0x80},
	"warm":    {0xff, 0xa0, 0x40},
	"claude":  {0xff, 0x8c, 0x66}, // Anthropic's terracotta, pushed to full brightness
}

// scale dims a colour to a fraction (0-1) of its brightness.
func (c rgb) scale(f float64) rgb {
	return rgb{byte(float64(c.r) * f), byte(float64(c.g) * f), byte(float64(c.b) * f)}
}

// hue returns the fully saturated, fully bright colour h degrees around the wheel.
func hue(h float64) rgb {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	h /= 60
	x := byte(255 * (1 - math.Abs(math.Mod(h, 2)-1)))
	switch int(h) {
	case 0:
		return rgb{255, x, 0}
	case 1:
		return rgb{x, 255, 0}
	case 2:
		return rgb{0, 255, x}
	case 3:
		return rgb{0, x, 255}
	case 4:
		return rgb{x, 0, 255}
	default:
		return rgb{255, 0, x}
	}
}

type flag struct {
	dev  *hid.Device
	quit <-chan os.Signal
	lock *os.File // held while this process drives the light, so mood can find and stop it
}

var lockPath = filepath.Join(os.TempDir(), "lux.lock")

func open() (*flag, error) {
	if err := hid.Init(); err != nil {
		return nil, err
	}
	dev, err := hid.OpenFirst(vendorID, productID)
	if err != nil {
		return nil, fmt.Errorf("no Luxafor Flag found: %w", err)
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	lock, _ := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if lock != nil {
		// Best effort: a second lux just runs unlocked. The lock dies with the process.
		syscall.FcntlFlock(lock.Fd(), syscall.F_SETLK, &syscall.Flock_t{Type: syscall.F_WRLCK})
	}
	return &flag{dev: dev, quit: quit, lock: lock}, nil
}

// mood is for scripts and hooks: it stops the lux holding the lock, waits for it
// to switch the light off, then starts args as a detached background effect.
// `lux mood off` stops and switches the light off. Exits 1, touching nothing,
// when no Flag is plugged in.
func mood(args []string) {
	found := false
	hid.Init()
	hid.Enumerate(vendorID, productID, func(*hid.DeviceInfo) error { found = true; return nil })
	if !found {
		fmt.Fprintln(os.Stderr, "no Luxafor Flag found")
		os.Exit(1)
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for i := 0; i < 100; i++ {
		fl := syscall.Flock_t{Type: syscall.F_WRLCK}
		if syscall.FcntlFlock(lock.Fd(), syscall.F_GETLK, &fl) != nil || fl.Type == syscall.F_UNLCK {
			break
		}
		if i == 0 {
			syscall.Kill(int(fl.Pid), syscall.SIGINT)
		}
		time.Sleep(5 * time.Millisecond)
	}
	lock.Close()
	if len(args) == 0 || args[0] == "off" {
		args = []string{"off"} // a child switches the light off even when nothing was running
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// set lights one LED (1-6, front, back or allLEDs) in a colour.
func (f *flag) set(led byte, c rgb) {
	// Leading 0 is the HID report ID; the Flag has none. Command 1 = static colour.
	report := []byte{0, 1, led, c.r, c.g, c.b, 0, 0, 0}
	// The Flag rejects roughly 1 in 150 reports with a bare IOKit error; a short retry clears it.
	var err error
	for try := 0; try < 5; try++ {
		if _, err = f.dev.Write(report); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "write:", err)
	os.Exit(1)
}

// sleep waits d, or switches the light off and exits if Ctrl-C arrives first.
func (f *flag) sleep(d time.Duration) {
	select {
	case <-time.After(d):
	case <-f.quit:
		f.set(allLEDs, off)
		os.Exit(0)
	}
}

// pulse breathes a colour between 30% and 100%, so it never looks dim.
func pulse(f *flag, c rgb) {
	for t := 0.0; ; t += 0.05 {
		f.set(allLEDs, c.scale(0.3+0.7*(0.5+0.5*math.Sin(t))))
		f.sleep(20 * time.Millisecond)
	}
}

// strobe flashes a colour with the on-flash longer than the off-gap, so it looks intense.
func strobe(f *flag, c rgb) {
	for {
		f.set(allLEDs, c)
		f.sleep(70 * time.Millisecond)
		f.set(allLEDs, off)
		f.sleep(50 * time.Millisecond)
	}
}

// rainbow slides the whole light around the colour wheel.
func rainbow(f *flag, _ rgb) {
	for h := 0.0; ; h += 2 {
		f.set(allLEDs, hue(h))
		f.sleep(15 * time.Millisecond)
	}
}

// police flashes red and blue on opposite ends in triple bursts, then swaps sides.
func police(f *flag, _ rgb) {
	red, blue := colors["red"], colors["blue"]
	for {
		for i := 0; i < 3; i++ {
			f.set(front, red)
			f.set(back, blue)
			f.sleep(60 * time.Millisecond)
			f.set(allLEDs, off)
			f.sleep(40 * time.Millisecond)
		}
		red, blue = blue, red
	}
}

// disco gives every LED its own random colour, ten times a second.
func disco(f *flag, _ rgb) {
	for {
		for led := byte(1); led <= 6; led++ {
			f.set(led, hue(rand.Float64()*360))
		}
		f.sleep(100 * time.Millisecond)
	}
}

// chase runs a dot around the six LEDs with a fading tail, shifting hue as it goes
// unless given a colour.
func chase(f *flag, c rgb) {
	for i, h := 0, 0.0; ; i, h = i+1, h+7 {
		col := hue(h)
		if c != off {
			col = c
		}
		for led := 0; led < 6; led++ {
			age := (i - led + 6) % 6 // 0 = head, 5 = end of the tail
			f.set(byte(led+1), col.scale(math.Pow(0.4, float64(age))))
		}
		f.sleep(60 * time.Millisecond)
	}
}

// fire flickers every LED between red, orange and yellow at random brightness.
func fire(f *flag, _ rgb) {
	for {
		for led := byte(1); led <= 6; led++ {
			f.set(led, hue(rand.Float64()*40).scale(0.4+0.6*rand.Float64()))
		}
		f.sleep(time.Duration(40+rand.Intn(60)) * time.Millisecond)
	}
}

// rave strobes like strobe, but every flash is a new colour from the wheel.
func rave(f *flag, _ rgb) {
	for h := 0.0; ; h += 47 {
		f.set(allLEDs, hue(h))
		f.sleep(70 * time.Millisecond)
		f.set(allLEDs, off)
		f.sleep(50 * time.Millisecond)
	}
}

// wave rolls a sine wave of brightness across the LEDs while the hue drifts,
// or in one colour when given.
func wave(f *flag, c rgb) {
	for t := 0.0; ; t += 0.15 {
		col := hue(t * 20)
		if c != off {
			col = c
		}
		for led := 0; led < 6; led++ {
			b := 0.5 + 0.5*math.Sin(t+float64(led))
			f.set(byte(led+1), col.scale(0.1+0.9*b))
		}
		f.sleep(30 * time.Millisecond)
	}
}

// heartbeat thumps twice in red, rests, and goes again.
func heartbeat(f *flag, _ rgb) {
	red := colors["red"]
	for {
		for i := 0; i < 2; i++ {
			f.set(allLEDs, red)
			f.sleep(100 * time.Millisecond)
			f.set(allLEDs, red.scale(0.15))
			f.sleep(120 * time.Millisecond)
		}
		f.sleep(600 * time.Millisecond)
	}
}

// thunder sits in a dim blue gloom and tears it open with random white lightning.
func thunder(f *flag, _ rgb) {
	gloom := colors["blue"].scale(0.08)
	for {
		f.set(allLEDs, gloom)
		f.sleep(time.Duration(300+rand.Intn(2500)) * time.Millisecond)
		for i := rand.Intn(4) + 1; i > 0; i-- {
			f.set(allLEDs, white)
			f.sleep(time.Duration(20+rand.Intn(60)) * time.Millisecond)
			f.set(allLEDs, gloom)
			f.sleep(time.Duration(30+rand.Intn(120)) * time.Millisecond)
		}
	}
}

// matrix rains green: LEDs light up at random and decay until the next drop.
func matrix(f *flag, _ rgb) {
	green := colors["green"]
	var level [6]float64
	for {
		if rand.Intn(3) == 0 {
			level[rand.Intn(6)] = 1
		}
		for led := range level {
			f.set(byte(led+1), green.scale(level[led]))
			level[led] *= 0.8
		}
		f.sleep(50 * time.Millisecond)
	}
}

// effects run forever; Ctrl-C switches the light off. pulse and strobe take a colour.
var effects = map[string]func(*flag, rgb){
	"pulse":     pulse,
	"strobe":    strobe,
	"rainbow":   rainbow,
	"police":    police,
	"disco":     disco,
	"chase":     chase,
	"fire":      fire,
	"rave":      rave,
	"wave":      wave,
	"heartbeat": heartbeat,
	"thunder":   thunder,
	"matrix":    matrix,
}

const usage = `usage: lux <colour> | lux max [strobe] | lux pulse|strobe <colour> | lux <effect> | lux off | lux list

  lux red            solid colour
  lux claude         Claude orange
  lux max            every LED flat out white (ffffff)
  lux max strobe     strobe at full white
  lux pulse blue     breathe a colour, never below 30%
  lux strobe green   flash a colour
  lux rainbow        endless effect, Ctrl-C to stop (see lux list)
  lux chase claude   chase and wave take a colour too
  lux mood rainbow   swap the running effect for this one, in the background
  lux mood off       stop the background effect
  lux off
  lux list           colours and effects`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println(usage)
		os.Exit(2)
	}
	if args[0] == "mood" {
		mood(args[1:])
		return
	}
	if args[0] == "list" {
		fmt.Println("colours:", strings.Join(slices.Sorted(maps.Keys(colors)), " "), "max")
		fmt.Println("effects:", strings.Join(slices.Sorted(maps.Keys(effects)), " "))
		return
	}

	f, err := open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.dev.Close()

	cmd := args[0]
	var c rgb // the colour argument, if any
	if len(args) > 1 && cmd != "max" {
		var ok bool
		if c, ok = colors[args[1]]; !ok {
			fmt.Fprintln(os.Stderr, "unknown colour:", args[1])
			os.Exit(2)
		}
	}
	switch {
	case cmd == "off":
		f.set(allLEDs, off)
	case cmd == "max" && len(args) > 1 && args[1] == "strobe":
		strobe(f, white)
	case cmd == "max":
		f.set(allLEDs, white)
	case (cmd == "pulse" || cmd == "strobe") && c == off:
		fmt.Fprintf(os.Stderr, "lux %s needs a colour, e.g. lux %s red\n", cmd, cmd)
		os.Exit(2)
	default:
		if fx, ok := effects[cmd]; ok {
			fx(f, c)
		} else if col, ok := colors[cmd]; ok {
			f.set(allLEDs, col)
		} else {
			fmt.Fprintln(os.Stderr, "unknown command:", cmd)
			fmt.Println(usage)
			os.Exit(2)
		}
	}
}

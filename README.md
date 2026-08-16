# mofu

morph -> mofu

**mofu** converts a Live2D Cubism model export into a self-contained format,
and plays it back with [Ebitengine](https://ebitengine.org/).

It is not a Cubism SDK binding. If you want to drive a Live2D model at run
time — physics, expressions, arbitrary parameter control — use
[cubism-go](https://github.com/aethiopicuschan/cubism-go), which binds the
Cubism Core directly.

mofu takes the other road. A converter runs the Cubism Core **once, offline**,
samples every motion, and writes out the results. What is left is a set of
static meshes plus keyframed vertex positions: classic 3D vertex animation
with the handful of 2D extras a Live2D model needs — a texture index, clipping
masks, a render order, and Cubism's multiply/screen colours. Playing that back
is ordinary mesh interpolation.

The trade is deliberate:

* **The runtime links nothing proprietary.** Your game ships one `.mofu` file
  and this Go package. No Cubism Core, no dynamic library to locate, no
  platform-specific binaries to bundle, no SDK licence question at run time.
* **Playback is cheap and predictable.** Two frames get interpolated and
  drawn. There is no deformer to evaluate.
* **You give up run-time control.** Anything that is decided while the game is
  running cannot be baked. See [What does not survive baking](#what-does-not-survive-baking).

## Requirements

Converting needs the **Live2D Cubism Core** dynamic library. It is
proprietary, is not redistributable, and is therefore not in this repository.
Download the *Cubism SDK for Native* from
[live2d.com](https://www.live2d.com/sdk/download/native/) and put the library
for your platform next to the `mofu` executable:

| Platform | File                            |
| -------- | ------------------------------- |
| Linux    | `libLive2DCubismCore.so`        |
| macOS    | `libLive2DCubismCore.dylib`     |
| Windows  | `Live2DCubismCore.dll`          |

It is loaded at run time with `dlopen`/`LoadLibrary` through
[purego](https://github.com/ebitengine/purego), so no cgo is involved and the
library is never linked into a binary. Search order:

1. `$MOFU_CUBISM_CORE`, if set, as a full path to the library.
2. The directory holding the `mofu` executable.
3. The current working directory.
4. Whatever the OS loader finds on its own search path.

**Playback needs none of this.** The `mofu` Go package has no dependency on
the Core.

## Install

```sh
go install github.com/eihigh/mofu/cmd/mofu@latest        # the converter
go install github.com/eihigh/mofu/cmd/mofu-play@latest   # an optional viewer
```

The two are separate binaries on purpose: the converter stays runnable on a
headless build machine, while the viewer links Ebitengine and needs a display.

## Convert

```sh
mofu core                                    # check which Core was found
mofu bake path/to/Hiyori.model3.json -o hiyori.mofu
mofu info hiyori.mofu -v
mofu-play hiyori.mofu
```

`mofu bake` flags:

| Flag              | Meaning                                                       |
| ----------------- | ------------------------------------------------------------- |
| `-o <path>`       | Output path. Defaults to the input's name with a `.mofu` suffix. |
| `-fps <n>`        | Sampling rate. `0` (the default) keeps each motion's own `Meta.Fps`. |
| `-motion <path>`  | Bake an extra `.motion3.json` not listed in the model3.json. Repeatable. |
| `-core <path>`    | Use a specific Cubism Core library.                            |
| `-raw`            | Skip gzip compression of the body.                             |
| `-q`              | Only report errors.                                            |

Every model gets an `@rest` animation: a single frame with all parameters at
their defaults.

## Play

```go
package main

import (
	"log"

	"github.com/eihigh/mofu"
	"github.com/hajimehoshi/ebiten/v2"
)

type Game struct {
	model  *mofu.Model
	player *mofu.Player
}

func (g *Game) Update() error {
	g.player.Update()
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	cw, ch := g.model.CanvasSize()
	sw, sh := float64(screen.Bounds().Dx()), float64(screen.Bounds().Dy())
	s := min(sw/cw, sh/ch)

	var op mofu.DrawOptions
	op.GeoM.Translate(-cw/2, -ch/2)
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(sw/2, sh/2)
	g.player.Draw(screen, &op)
}

func (g *Game) Layout(w, h int) (int, int) { return w, h }

func main() {
	model, err := mofu.LoadFile("hiyori.mofu")
	if err != nil {
		log.Fatal(err)
	}
	player := model.NewPlayer()
	if err := player.Play("Idle"); err != nil {
		log.Fatal(err)
	}
	if err := ebiten.RunGame(&Game{model, player}); err != nil {
		log.Fatal(err)
	}
}
```

A `Model` is immutable and shareable; a `Player` holds one instance's playback
state, so draw the same model many times by making several players.

Vertex positions come out in canvas pixels with the origin at the top left, so
`Model.CanvasSize` is the box to fit with `DrawOptions.GeoM`.

## What does not survive baking

Sampling ahead of time is what buys the runtime its independence, and it is
also exactly what these features cannot tolerate. `mofu bake` prints a warning
for each one it finds.

* **Physics** (`.physics3.json`) is a simulation driven by live input. There
  is nothing to sample.
* **Pose** (`.pose3.json`) switches part visibility at run time.
* **Expressions** (`.exp3.json`) compose *on top of* whatever motion is
  playing. A baked frame is already composited, so they cannot be layered back
  on afterwards.
* **Arbitrary parameter control** — head tracking, lip sync driven by live
  audio, mouse following. Baked frames are the only poses available.

If you need any of these, you want a Core binding rather than this.

## The `.mofu` container

Written by `mofufmt`, which depends on neither the Cubism Core nor
Ebitengine.

```
header    "MOFU", version, flags        (16 bytes, uncompressed)
body      gzip, unless -raw
  canvas    size, origin, pixels-per-unit
  textures  the original image bytes, copied verbatim
  meshes    id, texture index, blend flags, mask list, UVs, indices,
            and the bounding box positions are quantised against
  animations
    per mesh: quantised vertex positions, opacity, render order,
              visibility, multiply and screen colour
```

Two things keep it small:

* **Positions are 16-bit.** Each mesh carries the bounding box covering every
  pose it takes in every animation, and positions are stored as fractions of
  it. That is half the size of floats, and the error is a 65535th of a mesh's
  own range.
* **Still channels are stored once.** A channel is written as a single sample
  until the frame where it first changes. In a typical model most drawables
  hold still through most motions, so most tracks cost one frame.

## Development

```sh
go build ./...
go test ./...
```

The tests never touch the real Cubism Core. `internal/fakecore` compiles a
small C stub implementing the same entry points over a hard-coded model, which
is enough to exercise the binding, the alignment requirements and the whole
bake pipeline. Tests needing it skip when no C compiler is available.

Tests for the `mofu` package itself import Ebitengine, whose package
initialiser needs a display; on a headless machine run them under
`xvfb-run -a go test ./...`.

## Licence

MIT. See [LICENSE](LICENSE).

The Live2D Cubism Core and any Live2D model assets are covered by Live2D's own
terms, not by this licence.

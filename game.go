package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	Width  = 800
	Height = 600
)

type Game struct {
	rom, chrrom                  []byte     // Points to the start of PRG/CHR ROM
	prg                          [4]byte    // Current PRG/CHR banks
	chr                          [8]byte    //
	prgbits, chrbits             byte       // Number of bits per PRG/CHR bank
	a, x, y, p, s, pch, pcl      byte       // CPU registers
	addrLo, addrHi               byte       // Current instruction address
	nomem                        byte       // 1 => current instruction doesn't write to memory
	result                       byte       // Temp variable
	val                          byte       // Current instruction value
	cross                        byte       // 1 => page crossing occurred
	tmp                          byte       // Temp variables
	ppumask, ppuctrl, ppustatus  byte       // PPU registers
	ppubuf                       byte       // PPU buffered reads
	w                            byte       // Write toggle PPU register
	fineX                        byte       // X fine scroll offset, 0..7
	opcode                       byte       // Current instruction opcode
	nmiIRQ                       byte       // IRQ/NMI flag
	ntb                          byte       // Nametable byte
	ptbLo                        byte       // Pattern table lowbyte
	vram                         [2048]byte // Nametable RAM
	paletteram                   [64]byte   // Palette RAM
	ram                          [8192]byte // CPU RAM
	chrram                       [8192]byte // CHR RAM (only used for some games)
	prgram                       [8192]byte // PRG RAM (only used for some games)
	oam                          [256]byte  // Object Attribute Memory (sprite RAM)
	mask                         [20]byte   // Masks used in branch instructions
	keys                         byte       // Joypad shift register
	mirror                       byte       // Current mirroring mode
	mmc1Bits, mmc1Data, mmc1Ctrl byte       // Mapper 1 (MMC1) registers
	mmc3Chrprg                   [8]byte    // Mapper 3 (MMC3) registers
	mmc3Bits, mmc3Irq, mmc3Latch byte       //
	chrbank0, chrbank1, prgbank  byte       // Current PRG/CHR bank
	rombuf                       []byte     // Buffer to read ROM file into
	keyState                     []byte

	scany            uint16        // Scanline Y
	t, v             uint16        // "Loopy" PPU registers
	sum              uint16        // Sum used for ADC/SB
	dot              uint16        // Horizontal position of PPU, from 0..340
	atb              uint16        // Attribute byte
	shiftHi, shiftLo uint16        // Pattern table shift registers
	cycles           uint16        // Cycle count for current instruction
	frameBuffer      [61440]uint16 // 256x240 pixel frame buffer. Top and bottom 8 rows are not drawn.

	shiftAt int
}

// If `write` is non-zero, writes `val` to the address `hi:lo`, otherwise reads
// a value from the address `hi:lo`.

func (g *Game) mem(lo, hi, val, write byte) byte {
	var addr uint16 = uint16(hi)<<8 | uint16(lo)
	_ = addr
	//TODO: this
	return 39 // Miku number :)
}

func NewGame(rom []byte) (*Game, error) {
	g := &Game{}
	g.prgbits = 14
	g.chrbits = 12
	g.p = 4
	g.s = 0xfd
	g.mask = [20]byte{128, 64, 1, 2, 1, 0, 0, 1, 4, 0, 0, 4, 0, 0, 64, 0, 8, 0, 0, 8}
	g.rombuf = rom
	g.rom = rom[16:] // skip iNES header
	// PRG1 is the last bank. `rombuf[4]` is the number of 16k PRG banks.
	g.prg[1] = g.rombuf[4] - 1
	// CHR0 ROM is after all PRG data in the file. `rombuf[5]` is the number of
	// 8k CHR banks. If it is zero, assume the game uses CHR RAM.
	if g.rombuf[5] != 0 {
		g.chrrom = g.rom[int(g.rombuf[4])<<14:]
	} else {
		copy(g.chrrom, g.chrram[:])
	}
	// CHR1 is the last 4k bank.
	if g.rombuf[5] != 0 {
		g.chr[1] = g.rombuf[5]*2 - 1
	} else {
		g.chr[1] = 1
	}
	// Bit 0 of `rombuf[6]` is 0=>horizontal mirroring, 1=>vertical mirroring.
	g.mirror = 3 - g.rombuf[6]&1
	if g.rombuf[6]/16 == 4 {
		g.mem(0, 128, 0, 1) // Update to default mmc3 banks
		g.prgbits--         // 8kb PRG banks
		g.chrbits -= 2      // 1kb CHR banks
	}

	// Start at address in reset vector, at $FFFC.
	g.pcl = g.mem(0xfc, 0xff, 0, 0)
	g.pch = g.mem(0xfe, 0xff, 0, 0)

	return g, nil
}

func (g *Game) InitEbiten() {
	ebiten.SetWindowSize(Width, Height)
	ebiten.SetWindowTitle("smolnes-go")
}

func (g *Game) Start() error {
	return ebiten.RunGame(g)
}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {

}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return Width, Height
}

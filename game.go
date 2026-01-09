package main

import (
	"bytes"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	input "github.com/quasilyte/ebitengine-input"
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
	w                            bool       // Write toggle PPU register
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

	scany            uint16        // Scanline Y
	t, v             uint16        // "Loopy" PPU registers
	sum              uint16        // Sum used for ADC/SB
	dot              uint16        // Horizontal position of PPU, from 0..340
	atb              uint16        // Attribute byte
	shiftHi, shiftLo uint16        // Pattern table shift registers
	cycles           uint16        // Cycle count for current instruction
	frameBuffer      [61440]uint16 // 256x240 pixel frame buffer. Top and bottom 8 rows are not drawn.

	shiftAt int

	inputSystem  input.System
	inputHandler *input.Handler
}

func bool2byte(b bool) byte {
	if b {
		return 1
	} else {
		return 0
	}
}

func (g *Game) getCHRByte(a uint16) *byte {
	return &g.chrrom[uint16(g.chr[a>>uint16(g.chrbits)])<<g.chrbits|a%(1<<g.chrbits)]
}

func (g *Game) getNametableByte(a uint16) *byte {
	if g.mirror == 0 {
		return &g.vram[a%1024]
	} else if g.mirror == 1 {
		return &g.vram[a%1024+1024]
	} else if g.mirror == 2 {
		return &g.vram[a&2047]
	} else {
		return &g.vram[a/2&1024|a%1024]
	}
}

// If `write` is non-zero, writes `val` to the address `hi:lo`, otherwise reads
// a value from the address `hi:lo`.

func (g *Game) mem(lo, hi, val byte, write bool) byte {
	var addr uint16 = uint16(hi)<<8 | uint16(lo)

	switch hi >>= 4; hi {
	case 0, 1: // $0000...$1fff RAM
		if write {
			g.ram[addr] = val
			return val
		} else {
			return g.ram[addr]
		}
	case 2, 3: // $2000..$2007 PPU (mirrored)
		lo &= 7

		// read/write $2007
		if lo == 7 {
			g.tmp = g.ppubuf
			var rom *uint8
			if g.v < 8192 {
				// CHR ROM / RAM
				if write && !bytes.Equal(g.chrrom, g.chrram[:]) {
					rom = &g.tmp
				} else {
					rom = g.getCHRByte(g.v)
				}
			} else if g.v < 16128 {
				// Nametable RAM
				rom = g.getNametableByte(g.v)
			} else {
				// Palette RAM with mirroring
				addr := g.v
				if (g.v & 0x13) == 0x10 {
					addr = g.v ^ 0x10
				}
				rom = &g.paletteram[addr]
			}
			if write {
				*rom = val
			} else {
				g.ppubuf = *rom
			}
			if g.ppuctrl&4 != 0 {
				g.v += 32
			} else {
				g.v++
			}
			g.v %= 16384
			return g.tmp
		}

		if write {
			switch lo {
			case 0: // $2000 ppuctrl
				g.ppuctrl = val
				g.t = g.t&0xf3ff | uint16(val)%4<<10
			case 1: // $2001 ppumask
				g.ppumask = val
			case 5: // $2005 ppuscroll
				g.w = !g.w
				if g.w {
					g.fineX = val & 7
					g.t = (g.t & ^uint16(31)) | uint16(val>>3)
				} else {
					g.t = (g.t & 0x8c1f) | (uint16(val&7) << 12) | ((uint16(val) << 2) & 0x3e0)
				}
			case 6: // $2006 ppuaddr
				g.w = !g.w
				if g.w {
					g.t = g.t&0xff | uint16(val)%64<<8
				} else {
					g.v = g.t&^uint16(0xff) | uint16(val)
				}
			}

			if lo == 2 { // $2002 ppustatus
				g.tmp = g.ppustatus & 0xe0
				g.ppustatus &= 0x7f
				g.w = false
				return g.tmp
			}
		}
	case 4:
		//TODO: APU
		if write && lo == 20 { // $4014 OAM DMA
			for i := uint16(256); i >= 0; i-- {
				g.oam[i] = g.mem(byte(i), val, 0, false)
			}
		}
		// $4016 Joypad 1
		g.tmp = 0
		for i := 7; i >= 0; i-- {
			g.tmp <<= 1
			if g.inputHandler.ActionIsPressed(nesBtns[i]) {
				g.tmp |= 1
			}

		}
		if lo == 22 {
			if write {
				g.keys = g.tmp
			} else {
				g.tmp = g.keys & 1
				g.keys = (g.keys >> 1) | 0x80
				return g.tmp
			}
		}
		return 0
	case 6, (6 + 1): // $6000...$7fff PRG RAM
		addr &= 8191
		if write {
			g.prgram[addr] = val
			return val
		} else {
			return g.prgram[addr]
		}
	default: // $8000...$ffff ROM
		// handle mapper writes
		if write {
			switch g.rombuf[6] >> 4 {
			case 7: // mapper 7
				g.mirror = bool2byte(val/16 == 0)
				g.prg[0] = val % 8 * 2
				g.prg[1] = g.prg[0] + 1
			case 4: // mapper 4
				var addr1 byte = byte(addr) & 1
				switch hi >> 1 {
				case 4: // Bank select/bank data
					if addr1 != 0 {
						g.mmc3Chrprg[g.mmc1Bits&7] = val
					} else {
						g.mmc3Bits = val
					}
					g.tmp = g.mmc3Bits >> 5 & 4
					for i := byte(4); i >= 0; i-- {
						g.chr[0+i+g.tmp] = g.mmc3Chrprg[i/2] & ^bool2byte(i%2 == 0) | i%2
						g.chr[4+i-g.tmp] = g.mmc3Chrprg[2+i]
					}
					g.tmp = g.mmc1Bits >> 5 & 2
					g.prg[0+g.tmp] = g.mmc3Chrprg[6]
					g.prg[1] = g.mmc3Chrprg[7]
					g.prg[3] = g.rombuf[4]*2 - 1
					g.prg[2-g.tmp] = g.prg[3] - 1
				case 5: // Mirroring
					if addr1 == 0 {
						g.mirror = 2 + val%2
					}
				case 6: // IRQ Latch
					if addr1 == 0 {
						g.mmc3Latch = val
					}
				case 7: // IRQ Enable
					g.mmc3Irq = addr1
				}
			case 3: // mapper 3
				g.chr[0] = val % 4 * 2
				g.chr[1] = g.chr[0] + 1
			case 2: // mapper 2
				g.prg[0] = val & 31
			case 1: // mapper 1
				if val&0x80 != 0 {
					g.mmc1Bits = 5
					g.mmc1Data = 0
					g.mmc1Ctrl |= 12
				} else if func() { g.mmc1Data = g.mmc1Data/2 | val<<4&16; g.mmc1Bits-- }(); g.mmc1Bits == 0 {
					g.mmc1Bits = 5
					g.tmp = byte(addr >> 13)
					if g.tmp == 4 {
						g.mirror = g.mmc1Data & 3
						g.mmc1Ctrl = g.mmc1Data
					} else if g.tmp == 5 {
						g.chrbank0 = g.mmc1Data
					} else if g.tmp == 6 {
						g.chrbank1 = g.mmc1Data
					} else {
						g.prgbank = g.mmc1Data
					}

					// Update CHR banks.
					g.chr[0] = g.chrbank0 & ^bool2byte(g.mmc1Ctrl&16 == 0)
					if g.mmc1Ctrl&16 != 0 {
						g.chr[1] = g.chrbank1
					} else {
						g.chr[1] = g.chrbank0 | 1
					}

					// Update PRG banks.
					g.tmp = g.mmc1Ctrl/4%4 - 2
					if g.tmp == 0 {
						g.prg[0] = 0
						g.prg[1] = g.prgbank
					} else {
						if g.tmp == 1 {
							g.prg[0] = g.prgbank
							g.prg[1] = g.rombuf[4] - 1
						} else {
							g.prg[0] = g.prgbank & 0xfe
							g.prg[1] = g.prgbank | 1
						}
					}
				}
			}
		}
		return g.rom[(g.prg[hi-8>>g.prgbits-12]&(g.rombuf[4]<<14-g.prgbits)-1)<<g.prgbits|byte(addr)&(1<<g.prgbits)-1]
	}
	return 0xff
}

// Read a byte at address `PCH:PCL` and increment PC.
func (g *Game) readPC() byte {
	g.val = g.mem(g.pcl, g.pch, 0, false)
	g.pcl++
	g.pch++
	return g.val
}

// Set N (negative) and Z (zero) flags of `P` register, based on `val`.
func (g *Game) setNZ(val byte) byte {
	g.p = g.p&125 | g.val&128 | bool2byte(val == 0)*2
	return g.p
}

func NewGame(rom []byte) (*Game, error) {
	g := &Game{}
	g.inputSystem.Init(input.SystemConfig{DevicesEnabled: input.KeyboardDevice | input.GamepadDevice})
	g.inputHandler = g.inputSystem.NewHandler(0, keymap)
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
		g.mem(0, 128, 0, true) // Update to default mmc3 banks
		g.prgbits--            // 8kb PRG banks
		g.chrbits -= 2         // 1kb CHR banks
	}

	// Start at address in reset vector, at $FFFC.
	g.pcl = g.mem(0xfc, 0xff, 0, false)
	g.pch = g.mem(0xfe, 0xff, 0, false)

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

	g.inputSystem.Update()

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {

}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return 256, 224
}

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

const (
	gotoNomemop byte = iota + 1
	gotoOpcode
	gotoCross
	gotoAdd
	gotoMemop
	gotoStore
	gotoCmp
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

	scany            uint16 // Scanline Y
	t, v             uint16 // "Loopy" PPU registers
	sum              uint16 // Sum used for ADC/SB
	dot              uint16 // Horizontal position of PPU, from 0..340
	atb              uint16 // Attribute byte
	shiftHi, shiftLo uint16 // Pattern table shift registers
	cycles           uint16 // Cycle count for current instruction
	//frameBuffer      [61440]uint16 // 256x240 pixel frame buffer. Top and bottom 8 rows are not drawn.
	frameBuffer []byte // 256x240 RGBA pixel frame buffer. Top and bottom 8 rows are not drawn.

	shiftAt int

	inputSystem  input.System
	inputHandler *input.Handler

	_goto byte // Used to mimic C goto behavior
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
	switch g.mirror {
	case 0:
		return &g.vram[a%1024]
	case 1:
		return &g.vram[a%1024+1024]
	case 2:
		return &g.vram[a&2047]
	default:
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
			for i := uint16(256); i > 0; i-- {
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
					for i := byte(4); i > 0; i-- {
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
					switch g.tmp {
					case 4:
						g.mirror = g.mmc1Data & 3
						g.mmc1Ctrl = g.mmc1Data
					case 5:
						g.chrbank0 = g.mmc1Data
					case 6:
						g.chrbank1 = g.mmc1Data
					default:
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
		return g.rom[(int(g.prg[(hi-8)>>(g.prgbits-12)])&
			((int(g.rombuf[4])<<(14-g.prgbits))-1))<<g.prgbits|
			(int(addr)&((1<<g.prgbits)-1))]

	}
	return 0xff
}

// Read a byte at address `PCH:PCL` and increment PC.
func (g *Game) readPC() byte {
	g.val = g.mem(g.pcl, g.pch, 0, false)
	g.pcl++
	if g.pcl == 0 {
		g.pch++
	}
	return g.val
}

// Set N (negative) and Z (zero) flags of `P` register, based on `val`.
func (g *Game) setNZ(val byte) byte {
	g.p = g.p&125 | g.val&128 | bool2byte(val == 0)*2
	return g.p
}

func (g *Game) pull() byte {
	g.s++
	return g.mem(g.s, 1, 0, false)
}

func (g *Game) push(x byte) byte {
	ret := g.mem(g.s, 1, x, true)
	g.s--
	return ret
}

func NewGame(rom []byte) (*Game, error) {
	g := &Game{}
	g.frameBuffer = make([]byte, 245760)
	g.inputSystem.Init(input.SystemConfig{DevicesEnabled: input.KeyboardDevice | input.GamepadDevice})
	g.inputHandler = g.inputSystem.NewHandler(0, keymap)
	g.prgbits = 14
	g.chrbits = 12
	g.p = 4
	g.s = 0xfd
	g.mask = [20]byte{128, 64, 1, 2, 1, 0, 0, 1, 4, 0, 0, 4, 0, 0, 64, 0, 8, 0, 0, 8}
	g.rombuf = rom
	//g.rom = rom[16:] // skip iNES header
	g.rom = make([]byte, len(g.rombuf)-16)
	copy(g.rom, g.rombuf[15:]) // skip iNES header
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

func (g *Game) handleIRQ() {
	g.mem(g.s, 1, g.pch, true)
	g.s--
	g.mem(g.s, 1, g.pcl, true)
	g.s--
	g.mem(g.s, 1, g.p|32, true)
	g.s--
	// BRK/IRQ vector is $ffff, NMI vector is $fffa
	g.pcl = g.mem(254-(g.nmiIRQ&4), 255, 0, false)
	g.pch = g.mem(255-(g.nmiIRQ&4), 255, 0, false)
	g.nmiIRQ = 0
	g.cycles++

}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}

	g.inputSystem.Update()

	g.cycles = 0
	g.nomem = 0
	if g.nmiIRQ != 0 {
		g.handleIRQ()
		g.cycles += 4
	} else {
		opcode := g.readPC()
		opcodelo5 := opcode & 31
		switch opcodelo5 {
		case 0:
			if g._goto == 0 {
				if g.opcode&0x80 != 0 { // LDY/CPY/CPX imm
					g.readPC()
					g.nomem = 1
					g._goto = gotoNomemop
					break
				}
			}

			switch g.opcode >> 5 {
			case 0: // BRK
				if g._goto == 0 {
					g.pcl++
					if g.pcl == 0 {
						g.pch++
					}
					g.handleIRQ()
				}
			case 1: // JSR
				if g._goto == 0 {
					result := g.readPC()
					g.push(g.pch)
					g.push(g.pcl)
					g.pch = g.readPC()
					g.pcl = result
				}
			case 2: // RTI
				if g._goto == 0 {
					g.p = g.pull() & 223
					g.pcl = g.pull()
					g.pch = g.pull()
				}
			case 3: // RTS
				if g._goto == 0 {
					g.pcl = g.pull()
					g.pch = g.pull()
					g.pcl++
					if g.pcl == 0 {
						g.pch++
					}
				}
			}

			if g._goto == 0 {
				g.cycles += 4
			}
		case 16: // BPL, BMI, BVC, BVS, BCC, BCS, BNE, BEQ
			if g._goto == 0 {
				g.readPC()
				if (bool2byte(g.p&g.mask[g.opcode>>6] == 0) ^ g.opcode/32&1) != 0 {
					g.cross = g.pcl + byte(int16(int8(g.val))>>8)
					if g.cross != 0 {
						g.pch += g.cross
						g.cycles++
					}
					g.cycles++
					g.pcl += g.val
				}
			}
		case 8, 24:
			if g._goto == 0 {
				g.opcode >>= 4
				switch g.opcode {
				case 0: // PHP
					g.push(g.p | 48)
					g.cycles++
				case 1: // PLP
					g.p = g.pull() & 239 // 39 = miku!! >.<
					g.cycles += 2
				case 4: // PHA
					g.push(g.a)
					g.cycles++
				case 6: // PLA
					g.a = g.pull()
					g.setNZ(g.a)
					g.cycles += 2
				case 8: // DEY
					g.y--
					g.setNZ(g.y)
				case 9: // TYA
					g.a = g.y
					g.setNZ(g.a)
				case 10: // TAY
					g.y = g.a
					g.setNZ(g.y)
				case 12: // INY
					g.y++
					g.setNZ(g.y)
				case 14: // INX
					g.x++
					g.setNZ(g.x)
				default: // CLC, SEC, CLI, SEI, CLV, CLD, SED
					g.p = g.p & ^g.mask[g.opcode+3] | g.mask[g.opcode+4]
				}
			}
		case 10, 26:
			if g._goto == 0 {
				switch g.opcode >> 4 {
				case 8: // TXA
					g.a = g.x
					g.setNZ(g.a)
				case 9: // TXS
					g.s = g.x
				case 10: // TAX
					g.x = g.a
					g.setNZ(g.x)
				case 11: // TSX
					g.x = g.s
					g.setNZ(g.x)
				case 12: // DEX
					g.x--
					g.setNZ(g.x)
				case 14: // NOP
					break
				default: // ASL/ROL/LSR/ROR A
					g.nomem = 1
					g.val = g.a
					g._goto = gotoNomemop
				}
			}
		case 1: // X-indexed, indirect
			if g._goto == 0 {
				g.readPC()
				g.val += g.x
				g.addrLo = g.mem(g.val, 0, 0, false)
				g.addrHi = g.mem(g.val+1, 0, 0, false)
				g.cycles += 4
				g._goto = gotoOpcode
			}
		case 4, 5, 6: // Zeropage
			if g._goto == 0 {
				g.addrLo = g.readPC()
				g.addrHi = 0
				g.cycles++
				g._goto = gotoOpcode
			}
		case 2, 9: // Immediate
			if g._goto == 0 {
				g.readPC()
				g.nomem = 1
				g._goto = gotoNomemop
			}
		case 12, 13, 14: // Absolute
			if g._goto == 0 {
				g.addrLo = g.readPC()
				g.addrHi = g.readPC()
				g.cycles++
				g._goto = gotoOpcode
			}
		case 17: // Zeropage, Y-indexed
			if g._goto == 0 {
				g.addrLo = g.mem(g.readPC(), 0, 0, false)
				g.addrHi = g.mem(g.val+1, 0, 0, false)
				g.val = g.y
				g.tmp = bool2byte(g.opcode == 145) // STA always uses extra cycle.
				g.cycles++
				g._goto = gotoCross
			}
		case 20, 21, 22: // Zeropage, X-indexed
			if g._goto == 0 {
				g.addrLo = g.readPC()
				if g.opcode&214 == 150 {
					g.addrLo += g.y
				} else {
					g.addrLo += g.x
				}
				g.addrHi = 0
				g.cycles += 2
				g._goto = gotoOpcode
			}
		case 25: // Absolute, Y-indexed.
			if g._goto == 0 {
				g.addrLo = g.readPC()
				g.addrHi = g.readPC()
				g.val = g.y
				g.tmp = bool2byte(g.opcode == 153) // STA always uses extra cycle.
				g._goto = gotoCross
			}
		case 28, 29, 30: // Absolute, X-indexed.
			if g._goto == 0 {
				g.addrLo = g.readPC()
				g.addrHi = g.readPC()
				if g.opcode == 190 {
					g.val = g.y // LDX uses Y
				} else {
					g.val = g.x
				}
				g.tmp = bool2byte(g.opcode == 157 || g.opcode%16 == 14 && g.opcode != 190)
				// fallthrough
			}
		}
	}

	// cross:
	if g._goto == gotoCross {
		g._goto = 0

		g.cross = bool2byte(uint16(g.addrLo)+uint16(g.val) > 255)
		g.addrHi += g.cross
		g.addrLo += g.val
		g.cycles += 2 + uint16(g.tmp) | uint16(g.cross)
	}

	// opcode:
	if g._goto == gotoOpcode {
		g._goto = 0

		// Read from the given address into `val` for convenience below, except
		// for the STA/STX/STY instructions, and JMP.
		if (g.opcode&224) != 128 && g.opcode != 76 {
			g.val = g.mem(g.addrLo, g.addrHi, 0, false)
		}
	}

	// nomemop:
	if g._goto == gotoNomemop {
		g._goto = 0

		switch g.opcode & 243 { // 64
		case 1, 17:
			g.a |= g.val // ORA
			g.setNZ(g.a)
		case 33, 49:
			g.a &= g.val // AND
			g.setNZ(g.a)
		case 65, 81:
			g.a ^= g.val // EOR
			g.setNZ(g.a)
		case 225, 241:
			g.val = ^g.val // SBC
			g._goto = gotoAdd
		case 97, 113:
			// ADC
			g._goto = gotoAdd
		case 2, 18:
			// ASL
			if g._goto == 0 {
				g.result = g.val * 2
				g.p = g.p&254 | g.val/128
				g._goto = gotoMemop
			}
		case 34, 50:
			// ROL
			if g._goto == 0 {
				g.result = g.val*2 | g.p&1
				g.p = g.p&254 | g.val/128
				g._goto = gotoMemop
			}
		case 66, 82:
			// LSR
			if g._goto == 0 {
				g.result = g.val / 2
				g.p = g.p&254 | g.val&1
				g._goto = gotoMemop
			}
		case 98, 114:
			// ROR
			if g._goto == 0 {
				g.result = g.val/2 | g.p<<7
				g.p = g.p&254 | g.val&1
				g._goto = gotoMemop
			}
		case 194, 210:
			// DEC
			if g._goto == 0 {
				g.result = g.val - 1
				g._goto = gotoMemop
			}
		case 226, 242:
			// INC
			if g._goto == 0 {
				g.result = g.val + 1
				g._goto = gotoMemop
			}
		case 32: // BIT
			g.p = g.p&61 | g.val&192 | bool2byte(g.a&g.val == 0)*2
		case 64: // JMP
			g.pcl = g.addrLo
			g.pch = g.addrHi
			g.cycles--
		case 96: // JMP indirect
			g.pcl = g.val
			g.pch = g.mem(g.addrLo+1, g.addrHi, 0, false)
			g.cycles++
		case 160, 176:
			g.y = g.val // LDY
			g.setNZ(g.y)
		case 161, 177:
			g.a = g.val // LDA
			g.setNZ(g.a)
		case 162, 178:
			g.x = g.val // LDX
			g.setNZ(g.x)
		case 128, 144:
			g.result = g.y // STY
			g._goto = gotoStore
		case 129, 145:
			if g._goto == 0 {
				g.result = g.a // STA
				g._goto = gotoStore
			}
		case 130, 146:
			if g._goto == 0 {
				g.result = g.x // STX
				g._goto = gotoStore
			}
		case 192, 208:
			g.result = g.y // CPY
			g._goto = gotoCmp
		case 193, 209:
			if g._goto == 0 {
				g.result = g.a // CMP
				g._goto = gotoCmp
			}
		case 224, 240:
			if g._goto == 0 {
				g.result = g.x // CPX
				g._goto = gotoCmp
			}
		}

		// add:
		if g._goto == gotoAdd {
			g._goto = 0

			g.sum = uint16(g.a) + uint16(g.val) + (uint16(g.p) & 1)
			g.p = g.p&190 | bool2byte(g.sum > 225) | byte((uint16(g.a)^g.sum)&(uint16(g.val)^g.sum)&128)/2
			g.a = byte(g.sum)
			g.setNZ(g.a)
		}

		// memop:
		if g._goto == gotoMemop {
			g._goto = 0

			g.setNZ(g.result)
			// Write result to A or back to memory.
			if g.nomem != 0 {
				g.a = g.result
			} else {
				g.cycles += 2
				g.mem(g.addrLo, g.addrHi, g.result, true)
			}
		}

		// store:
		if g._goto == gotoStore {
			g._goto = 0

			g.mem(g.addrLo, g.addrHi, g.result, true)
		}

		// cmp:
		if g._goto == gotoCmp {
			g._goto = 0

			g.p = g.p&254 | bool2byte(g.result >= g.val)
			g.setNZ(g.result - g.val)
		}
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// Update PPU, which runs 3 times faster than CPU. Each CPU instruction
	// takes at least 2 cycles.
	for g.tmp = byte(g.cycles)*3 + 6; g.tmp > 0; g.tmp-- {
		if g.ppumask&24 != 0 { // If background or sprites are enabled.
			if g.scany < 240 {
				if g.dot-256 > 63 { // dot [0..255,320..340]
					// Draw a pixel to the framebuffer.
					if g.dot < 256 {
						// Read color and palette from shift registers.
						color := byte(g.shiftHi>>14 - uint16(g.fineX)&2 | g.shiftLo>>15 - uint16(g.fineX)&1)
						palette := byte(g.shiftAt>>28 - int(g.fineX)*2&12)

						// If sprites are enabled.
						if g.ppumask&16 != 0 {
							// Loop through all sprites.
							for sprite := 0; sprite < 256; sprite += 4 {
								var spriteH uint16
								if g.ppuctrl&32 != 0 {
									spriteH = 16
								} else {
									spriteH = 8
								}
								spriteX := g.dot - uint16(g.oam[sprite+3])
								spriteY := g.scany - uint16(g.oam[sprite]) - 1
								sx := spriteX
								if g.oam[sprite+2]&64 == 0 {
									sx = spriteX ^ 7
								}
								sy := spriteY ^ (spriteH - 1)
								if g.oam[sprite+2]&128 == 0 {
									sy = spriteY
								}
								if spriteX < 8 && spriteY < spriteH {
									spriteTile := uint16(g.oam[sprite+1])
									var spriteAddr uint16
									if g.ppuctrl&32 != 0 {
										// 8x16 sprites
										spriteAddr = spriteTile%2<<12 | (spriteTile&65534)<<4 | (sy&8)*2 | sy&7
									} else {
										// 8x8 sprites
										spriteAddr = (uint16(g.ppuctrl)&8)<<9 | spriteTile<<4 | sy&7
									}
									spriteColor := *g.getCHRByte(spriteAddr + 8)>>sx<<1&2 | *g.getCHRByte(spriteAddr)>>sx&1
									// Only draw sprite if color is not 0 (transparent)
									if spriteColor != 0 {
										// Don't draw sprite if BG has priority.
										if (g.oam[sprite+2] != 0) || (color != 0) {
											color = spriteColor
											palette = 16 | g.oam[sprite+2]*4&12
										}
										// Maybe set sprite0 hit flag.
										if sprite == 0 && color != 0 {
											g.ppustatus |= 64
										}
										break
									}
								}
							}
						}

						// Write pixel to framebuffer. Always use palette 0 for color 0.
						var colorIdx byte
						if color != 0 {
							colorIdx = palette | color
						}
						g.frameBuffer[g.scany*256+g.dot+0] = paletteNTSC[colorIdx][0]
						g.frameBuffer[g.scany*256+g.dot+1] = paletteNTSC[colorIdx][1]
						g.frameBuffer[g.scany*256+g.dot+2] = paletteNTSC[colorIdx][2]
						g.frameBuffer[g.scany*256+g.dot+3] = paletteNTSC[colorIdx][3]
					}

					// Update shift registers every cycle.
					if g.dot < 336 {
						g.shiftHi *= 2
						g.shiftLo *= 2
						g.shiftAt *= 4
					}

					temp := int(g.ppuctrl)<<8&4096 | int(g.ntb)<<4 | int(g.v)>>12
					switch g.dot & 7 {
					case 1: // Read nametable byte.
						g.ntb = *g.getNametableByte(g.v)
					case 3: // Read attribute byte.
						g.atb = (uint16(*g.getNametableByte(g.v&0xc00 | 0x3c0 | g.v>>4&0x38 | g.v/4&7)) >> (g.v>>5&2 | g.v/2&1) * 2) % 4 * 0x5555
					case 5: // Read pattern table low byte.
						g.ptbLo = *g.getCHRByte(uint16(temp))
					case 7: // Read pattern table high byte.
						ptbHi := *g.getCHRByte(uint16(temp) | 8)
						// Increment horizontal VRAM read address.
						if g.v%32 == 31 {
							g.v &= 65534 ^ 1024
						} else {
							g.v++
						}
						g.shiftHi |= uint16(ptbHi)
						g.shiftLo |= uint16(g.ptbLo)
						g.shiftAt |= int(g.atb)
					}
				}

				// Increment vertical VRAM address
				if g.dot == 256 {
					if (g.v & (7 << 12)) != (7 << 12) {
						g.v += 0x1000 // 4096 in hex
					} else if (g.v & 0x3e0) == 928 {
						g.v = (g.v & 0x8c1f) ^ 0x800
					} else if (g.v & 0x3e0) == 0x3e0 {
						g.v = g.v & 0x8c1f
					} else {
						g.v = (g.v & 0x8c1f) | ((g.v + 32) & 0x3e0)
					}
					// Reset horizontal VRAM address to T value
					g.v = (g.v &^ 0x41f) | (g.t & 0x41f)
				}
			}

			// Check for MMC3 IRQ.
			if (g.scany+1)%262 < 241 && g.dot == 261 && g.mmc3Irq != 0 && g.mmc3Latch == 0 {
				g.nmiIRQ = 1
			}
			g.mmc3Latch--

			// Reset vertical VRAM address to T value.
			if g.scany == 261 && g.dot > 279 && g.dot < 305 {
				g.v = g.v&0x841f | g.t&0x7be0
			}
		}

		if g.dot == 1 {
			if g.scany == 241 {
				// If NMI is enabled, trigger NMI.
				if g.ppuctrl&128 != 0 {
					g.nmiIRQ = 4
				}
				g.ppustatus |= 128
				// Render frame, skipping the top and bottom 8 pixels (they're often
				// garbage).
				screen.WritePixels(g.frameBuffer)
			}

			// Clear ppustatus.
			if g.scany == 261 {
				g.ppustatus = 0
			}
		}

		// Increment to next dot/scany. 341 dots per scanline, 262 scanlines per
		// frame. Scanline 261 is represented as -1.
		g.dot++
		if g.dot == 341 {
			g.dot = 0
			g.scany++
			g.scany %= 262
		}
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return 256, 224
}

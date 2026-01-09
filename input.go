package main

import input "github.com/quasilyte/ebitengine-input"

const (
	ActionA input.Action = iota
	ActionB
	ActionSelect
	ActionStart
	ActionUp
	ActionDown
	ActionLeft
	ActionRight
)

var keymap input.Keymap = input.Keymap{
	ActionA:      {input.KeyX, input.KeyGamepadA},
	ActionB:      {input.KeyZ, input.KeyGamepadB},
	ActionSelect: {input.KeyTab, input.KeyGamepadHome},
	ActionStart:  {input.KeyEnter, input.KeyGamepadStart},
	ActionUp:     {input.KeyUp, input.KeyGamepadUp},
	ActionDown:   {input.KeyDown, input.KeyGamepadDown},
	ActionLeft:   {input.KeyLeft, input.KeyGamepadLeft},
	ActionRight:  {input.KeyRight, input.KeyGamepadRight},
}

var nesBtns = []input.Action{
	ActionA,
	ActionB,
	ActionSelect,
	ActionStart,
	ActionUp,
	ActionDown,
	ActionLeft,
	ActionRight,
}

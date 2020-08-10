package main

import "log"

var genReason = []string{
	"not running",
	"front panel",
	"remote run request",
	"run schedule",
	"high inverter temp",
	"impending inverter shutdown",
	"synchronisation fault",
	"state of charge",
	"low battery volts",
	"battery midpoint voltage error",
	"equalising battery",
	"high AC load",
	"generator exercise",
	"generator available",
	"generator fault",
	"generator lockout active",
	"battery float",
	"cooling down",
	"confirmed start",
	"manual",
	"AC source present",
	"disabled",
	"support mode",
	"equalise",
	"battery load",
	"warming up",
}

func splinkGeneratorReason(r int) string {
	if r >= 0 && r < len(genReason) {
		return genReason[r]
	}

	log.Printf("received invalid reason %d\n", r)
	return "invalid"
}

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

var chargingMode = []string{
	"initial",
	"bulk",
	"absorb",
	"short term float",
	"return to float",
	"equalise",
	"long term float",
}

func splinkChargingMode(r int) string {
	if r >= 0 && r < len(chargingMode) {
		return chargingMode[r]
	}

	log.Printf("received invalid charging mode %d\n", r)
	return "invalid"
}

var sourceStatus = []string{
	"AC source not present",
	"E-N link not detected",
	"source lockout active",
	"inverter lockout active",
	"capacity limit active",
	"charger lockout active",
	"outside operating range",
	"AC source in tolerance",
	"volts too high for freq.",
}

func splinkSourceStatus(r int) string {
	if r >= 0 && r < len(sourceStatus) {
		return sourceStatus[r]
	}

	log.Printf("received invalid source status %d\n", r)
	return "invalid"
}

package vocabulary_test

// THE UNIT TABLE ONLY GROWS.
//
// A stored quantity is a value and a unit code, read through the table: every
// filter, ordering, bound and title converts it with the factor pinned here.
// Remove a code or a measure and its rows refuse their next write; change a
// factor or an offset and every stored value in that unit means a different
// amount on read, silently, with no write and no changelog entry. Neither is a
// table edit: it is a migration of the rows. So this pin is the table as it
// shipped, and it moves one way: a new measure or code is added here in the
// same change that adds it to the table, and nothing here is ever changed or
// removed.
//
// Each entry is the exact factor to the measure's base, then the offset where
// there is one (base = value x factor + offset); "none" is a code no rational
// factor reaches (a level, an arbitrary unit, rad against deg).

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

var pinnedUnits = map[string]map[string]string{
	"mass": {
		"kg":         "1",
		"g":          "1/1000",
		"mg":         "1/1000000",
		"ug":         "1/1000000000",
		"ng":         "1/1000000000000",
		"t":          "1000",
		"[lb_av]":    "45359237/100000000",
		"[oz_av]":    "45359237/1600000000",
		"[stone_av]": "317514659/50000000",
	},
	"length": {
		"m":       "1",
		"km":      "1000",
		"cm":      "1/100",
		"mm":      "1/1000",
		"nm":      "1/1000000000",
		"[in_i]":  "127/5000",
		"[ft_i]":  "381/1250",
		"[yd_i]":  "1143/1250",
		"[mi_i]":  "201168/125",
		"[nmi_i]": "1852",
	},
	"area": {
		"m2":      "1",
		"cm2":     "1/10000",
		"har":     "10000",
		"[sft_i]": "145161/1562500",
	},
	"volume": {
		"m3":       "1",
		"L":        "1/1000",
		"dL":       "1/10000",
		"mL":       "1/1000000",
		"[gal_us]": "473176473/125000000000",
		"[qt_us]":  "473176473/500000000000",
		"[pt_us]":  "473176473/1000000000000",
		"[cup_us]": "473176473/2000000000000",
		"[foz_us]": "473176473/16000000000000",
		"[tbs_us]": "473176473/32000000000000",
		"[tsp_us]": "157725491/32000000000000",
		"[gal_br]": "454609/100000000",
		"[foz_br]": "454609/16000000000",
		"[cup_m]":  "3/12500",
		"[tbs_m]":  "3/200000",
		"[tsp_m]":  "1/200000",
		"[cft_i]":  "55306341/1953125000",
	},
	"duration": {
		"s":   "1",
		"ms":  "1/1000",
		"us":  "1/1000000",
		"min": "60",
		"h":   "3600",
		"d":   "86400",
		"wk":  "604800",
		"mo":  "2629800",
		"a":   "31557600",
	},
	"temperature": {
		"K":      "1",
		"Cel":    "1 + 5463/20",
		"[degF]": "5/9 + 45967/180",
	},
	"temperature-difference": {
		"K":      "1",
		"Cel":    "1",
		"[degF]": "5/9",
	},
	"energy": {
		"J":        "1",
		"kJ":       "1000",
		"MJ":       "1000000",
		"cal":      "523/125",
		"kcal":     "4184",
		"[Cal]":    "4184",
		"W.h":      "3600",
		"kW.h":     "3600000",
		"eV":       "801088317/5000000000000000000000000000",
		"[Btu_IT]": "52752792631/50000000",
	},
	"power": {
		"W":          "1",
		"mW":         "1/1000",
		"kW":         "1000",
		"MW":         "1000000",
		"[HP]":       "37284993579113511/50000000000000",
		"kcal/d":     "523/10800",
		"kcal/h":     "523/450",
		"[Btu_IT]/h": "52752792631/180000000000",
	},
	"apparent-power": {
		"V.A": "1",
	},
	"reactive-power": {
		"V.A{reactive}": "1",
	},
	"pressure": {
		"Pa":        "1",
		"hPa":       "100",
		"kPa":       "1000",
		"bar":       "100000",
		"mbar":      "100",
		"atm":       "101325",
		"mm[Hg]":    "66661/500",
		"cm[H2O]":   "196133/2000",
		"[psi]":     "8896443230521/1290320000",
		"[in_i'Hg]": "8465947/2500",
	},
	"speed": {
		"m/s":      "1",
		"km/h":     "5/18",
		"[mi_i]/h": "1397/3125",
		"[kn_i]":   "463/900",
		"[ft_i]/s": "381/1250",
	},
	"pace": {
		"s/m":            "1",
		"min/km":         "3/50",
		"s/km":           "1/1000",
		"min/[mi_i]":     "625/16764",
		"s/(100.m)":      "1/100",
		"s/(100.[yd_i])": "25/2286",
		"s/(500.m)":      "1/500",
	},
	"acceleration": {
		"m/s2": "1",
		"[g]":  "196133/20000",
	},
	"frequency": {
		"Hz":  "1",
		"kHz": "1000",
	},
	"rate": {
		"/s":          "1",
		"/min":        "1/60",
		"/h":          "1/3600",
		"/d":          "1/86400",
		"/wk":         "1/604800",
		"{count}/min": "1/60",
	},
	"count": {
		"{count}": "1",
		"1":       "1",
	},
	"fraction": {
		"1":      "1",
		"%":      "1/100",
		"[ppth]": "1/1000",
		"[ppm]":  "1/1000000",
		"[ppb]":  "1/1000000000",
	},
	"amount-fraction": {
		"mmol/mol": "1/1000",
	},
	"score": {
		"{score}": "1",
	},
	"amount-of-substance": {
		"mol":  "1",
		"mmol": "1/1000",
		"umol": "1/1000000",
	},
	"amount-concentration": {
		"mol/L":  "1000",
		"mmol/L": "1",
		"umol/L": "1/1000",
		"nmol/L": "1/1000000",
		"pmol/L": "1/1000000000",
	},
	"mass-concentration": {
		"g/L":   "1",
		"g/dL":  "10",
		"mg/dL": "1/100",
		"mg/L":  "1/1000",
		"ug/L":  "1/1000000",
		"ng/mL": "1/1000000",
		"pg/mL": "1/1000000000",
		"ug/dL": "1/100000",
		"g/m3":  "1/1000",
		"mg/m3": "1/1000000",
		"ug/m3": "1/1000000000",
		"kg/m3": "1",
	},
	"number-concentration": {
		"/L":      "1000",
		"/uL":     "1000000000",
		"10*3/uL": "1000000000000",
		"10*6/uL": "1000000000000000",
		"10*9/L":  "1000000000000",
		"10*12/L": "1000000000000000",
	},
	"catalytic-concentration": {
		"U/L": "1/60000",
	},
	"catalytic-activity": {
		"kat": "1",
		"U":   "1/60000000",
	},
	"arbitrary": {
		"[IU]":    "none",
		"[IU]/L":  "none",
		"m[IU]/L": "none",
		"[IU]/mL": "none",
		"[arb'U]": "none",
	},
	"illuminance": {
		"lx": "1",
	},
	"luminous-flux": {
		"lm": "1",
	},
	"luminous-intensity": {
		"cd": "1",
	},
	"luminance": {
		"cd/m2": "1",
	},
	"irradiance": {
		"W/m2": "1",
	},
	"level": {
		"dB": "none",
	},
	"sound-pressure-level": {
		"dB[SPL]": "none",
	},
	"power-level": {
		"dB[W]": "none",
		"dBm":   "none",
	},
	"acidity": {
		"[pH]": "none",
	},
	"electric-potential": {
		"V":  "1",
		"mV": "1/1000",
	},
	"electric-current": {
		"A":  "1",
		"mA": "1/1000",
	},
	"electric-resistance": {
		"Ohm":  "1",
		"kOhm": "1000",
	},
	"electric-charge": {
		"C":    "1",
		"A.h":  "3600",
		"mA.h": "18/5",
	},
	"electric-conductivity": {
		"uS/cm": "1/10000",
	},
	"information": {
		"bit":  "1",
		"By":   "8",
		"kBy":  "8000",
		"MBy":  "8000000",
		"GBy":  "8000000000",
		"KiBy": "8192",
		"MiBy": "8388608",
		"GiBy": "8589934592",
	},
	"data-rate": {
		"bit/s":  "1",
		"kbit/s": "1000",
		"Mbit/s": "1000000",
		"By/s":   "8",
	},
	"angle": {
		"deg": "1",
		"'":   "1/60",
		"''":  "1/3600",
		"rad": "none",
	},
	"volume-flow": {
		"L/min":        "1/60000",
		"L/h":          "1/3600000",
		"mL/min":       "1/60000000",
		"m3/h":         "1/3600",
		"[gal_us]/min": "157725491/2500000000000",
	},
	"mass-flow": {
		"g/s": "1/1000",
	},
	"force": {
		"N":        "1",
		"[lbf_av]": "8896443230521/2000000000000",
	},
	"torque": {
		"N.m":             "1",
		"[lbf_av].[ft_i]": "3389544870828501/2500000000000000",
	},
	"activity-concentration": {
		"Bq/m3": "1",
		"pCi/L": "37",
	},
	"absorbed-dose": {
		"Gy":  "1",
		"mGy": "1/1000",
	},
	"dose-equivalent": {
		"Sv":  "1",
		"mSv": "1/1000",
		"uSv": "1/1000000",
	},
	"dose-equivalent-rate": {
		"uSv/h": "1/3600000000",
	},
	"radioactivity": {
		"Bq":  "1",
		"kBq": "1000",
		"MBq": "1000000",
	},
	"body-mass-index": {
		"kg/m2": "1",
	},
	"oxygen-uptake-rate": {
		"mL/(kg.min)": "1/60000000",
		"[MET]":       "7/120000000",
	},
	"specific-power": {
		"W/kg": "1",
	},
	"specific-energy": {
		"kcal/kg": "4184",
	},
	"energy-per-distance": {
		"kW.h/(100.km)": "36",
		"W.h/km":        "18/5",
	},
	"fuel-consumption": {
		"L/(100.km)": "1/100000000",
	},
	"fuel-economy": {
		"[mi_i]/[gal_us]": "48000000000/112903",
		"km/L":            "1000000",
	},
	"precipitation-rate": {
		"mm/h":     "1/3600000",
		"[in_i]/h": "127/18000000",
	},
}

func TestUnitTableOnlyGrows(t *testing.T) {
	shipped := map[string]vocabulary.Measure{}
	for _, m := range vocabulary.Measures() {
		shipped[m.Name] = m
	}
	for name, codes := range pinnedUnits {
		m, ok := shipped[name]
		if !ok {
			t.Errorf("measure %s was removed: a property declaring it no longer loads; keep it", name)
			continue
		}
		for code, want := range codes {
			listed := false
			for _, c := range m.Units {
				listed = listed || c == code
			}
			if !listed {
				t.Errorf("%s: %s was removed: its stored rows would refuse every write; keep the code, and migrate the rows if they must move", name, code)
				continue
			}
			if got := pinnedFactor(name, code); got != want {
				t.Errorf("%s: %s moved from %q to %q, which changes every stored value in it without a write; that is a migration, not a table edit", name, code, want, got)
			}
		}
	}
	for _, m := range vocabulary.Measures() {
		codes, pinned := pinnedUnits[m.Name]
		if !pinned {
			t.Errorf("measure %s is new: pin it here, with its codes, in the same change that adds it", m.Name)
			continue
		}
		for _, code := range m.Units {
			if _, ok := codes[code]; !ok {
				t.Errorf("%s: %s is new: pin it here as %q: %q, in the same change that adds it", m.Name, code, code, pinnedFactor(m.Name, code))
			}
		}
	}
}

func pinnedFactor(measure, code string) string {
	f, o, err := vocabulary.ToBase(measure, code)
	if err != nil {
		return "none"
	}
	s := f.RatString()
	if o.Sign() != 0 {
		s += " + " + o.RatString()
	}
	return s
}

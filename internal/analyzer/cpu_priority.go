package analyzer

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yourusername/migsug/internal/proxmox"
)

// CPUPriority represents CPU generation/priority information
type CPUPriority struct {
	Model       string // Full CPU model name
	ShortName   string // Shortened name for display
	Generation  int    // Generation number (higher = newer)
	ReleaseYear int    // Approximate release year
	Priority    int    // Priority score (higher = prefer for migration target)
	Family      string // CPU family (Intel Xeon, AMD EPYC, etc.)
}

// CPUPriorityInfo contains information about CPU priorities in the cluster
type CPUPriorityInfo struct {
	Priorities []CPUPriority
	MaxPriority int
	MinPriority int
}

// GetCPUPriority returns the priority score for a CPU model
// Higher score = newer/better CPU = preferred migration target
func GetCPUPriority(cpuModel string) int {
	info := parseCPUModel(cpuModel)
	return info.Priority
}

// GetCPUPriorityInfo returns detailed CPU priority information
func GetCPUPriorityInfo(cpuModel string) CPUPriority {
	return parseCPUModel(cpuModel)
}

// GetClusterCPUPriorities analyzes all CPUs in the cluster and returns priority info
func GetClusterCPUPriorities(cluster *proxmox.Cluster) CPUPriorityInfo {
	seen := make(map[string]bool)
	var priorities []CPUPriority

	for _, node := range cluster.Nodes {
		if node.CPUModel == "" || seen[node.CPUModel] {
			continue
		}
		seen[node.CPUModel] = true
		priorities = append(priorities, parseCPUModel(node.CPUModel))
	}

	// Sort by priority (highest first)
	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i].Priority > priorities[j].Priority
	})

	info := CPUPriorityInfo{Priorities: priorities}
	if len(priorities) > 0 {
		info.MaxPriority = priorities[0].Priority
		info.MinPriority = priorities[len(priorities)-1].Priority
	}

	return info
}

// parseCPUModel extracts generation and priority information from CPU model string
func parseCPUModel(model string) CPUPriority {
	info := CPUPriority{
		Model:      model,
		ShortName:  shortenModel(model),
		Priority:   100, // Default priority
		Generation: 0,
		Family:     "Unknown",
	}

	modelUpper := strings.ToUpper(model)

	// Intel Xeon Scalable (modern server CPUs)
	// 1st gen: Skylake (2017) - 8100/6100/5100/4100/3100 series
	// 2nd gen: Cascade Lake (2019) - 8200/6200/5200/4200/3200 series
	// 3rd gen: Ice Lake (2021) - 8300/6300/5300/4300/3300 series
	// 4th gen: Sapphire Rapids (2023) - 8400/6400/5400/4400/3400 series
	// 5th gen: Emerald Rapids (2024) - 8500/6500/5500/4500/3500 series
	if strings.Contains(modelUpper, "XEON") {
		info.Family = "Intel Xeon"

		// Check for Scalable processors (model numbers like 8180, 6248, 5318Y, etc.)
		scalablePattern := regexp.MustCompile(`[8765432]([0-5])\d{2}`)
		if matches := scalablePattern.FindStringSubmatch(modelUpper); len(matches) > 1 {
			gen, _ := strconv.Atoi(matches[1])
			info.Generation = gen + 1 // 81xx = 1st gen, 82xx = 2nd gen, etc.

			switch gen {
			case 0: // 8000 series (actually means different things)
				info.Priority = 150
				info.ReleaseYear = 2017
			case 1: // x1xx - 1st gen Scalable (Skylake)
				info.Priority = 200
				info.ReleaseYear = 2017
			case 2: // x2xx - 2nd gen Scalable (Cascade Lake)
				info.Priority = 300
				info.ReleaseYear = 2019
			case 3: // x3xx - 3rd gen Scalable (Ice Lake)
				info.Priority = 400
				info.ReleaseYear = 2021
			case 4: // x4xx - 4th gen Scalable (Sapphire Rapids)
				info.Priority = 500
				info.ReleaseYear = 2023
			case 5: // x5xx - 5th gen Scalable (Emerald Rapids)
				info.Priority = 600
				info.ReleaseYear = 2024
			}
			return info
		}

		// Check for older E5/E7 v1-v4 processors
		if strings.Contains(modelUpper, "E5") || strings.Contains(modelUpper, "E7") {
			info.Family = "Intel Xeon E5/E7"
			if strings.Contains(modelUpper, "V4") {
				info.Generation = 4
				info.Priority = 140
				info.ReleaseYear = 2016
			} else if strings.Contains(modelUpper, "V3") {
				info.Generation = 3
				info.Priority = 130
				info.ReleaseYear = 2014
			} else if strings.Contains(modelUpper, "V2") {
				info.Generation = 2
				info.Priority = 120
				info.ReleaseYear = 2013
			} else if strings.Contains(modelUpper, "V1") || !strings.Contains(modelUpper, "V") {
				info.Generation = 1
				info.Priority = 110
				info.ReleaseYear = 2012
			}
			return info
		}

		// Check for E3 (entry-level server)
		if strings.Contains(modelUpper, "E3") {
			info.Family = "Intel Xeon E3"
			info.Priority = 100
			info.ReleaseYear = 2011
			return info
		}

		// Check for W series (workstation)
		if strings.Contains(modelUpper, "W-") {
			info.Family = "Intel Xeon W"
			// W-3300 series (2021), W-2200/W-3200 (2019), W-2100 (2017)
			wPattern := regexp.MustCompile(`W-(\d)(\d)\d{2}`)
			if matches := wPattern.FindStringSubmatch(modelUpper); len(matches) > 2 {
				series, _ := strconv.Atoi(matches[1])
				gen, _ := strconv.Atoi(matches[2])
				info.Generation = gen
				if series == 3 && gen >= 3 {
					info.Priority = 380
					info.ReleaseYear = 2021
				} else if gen >= 2 {
					info.Priority = 280
					info.ReleaseYear = 2019
				} else {
					info.Priority = 180
					info.ReleaseYear = 2017
				}
			}
			return info
		}
	}

	// AMD EPYC processors
	// 1st gen: Naples (2017) - 7001 series (7251, 7401, 7501, 7601, etc.)
	// 2nd gen: Rome (2019) - 7002 series (7252, 7402, 7502, 7702, etc.)
	// 3rd gen: Milan (2021) - 7003 series (7313, 7413, 7543, 7713, etc.)
	// 4th gen: Genoa (2022) - 9004 series (9124, 9274, 9374, 9654, etc.)
	// 5th gen: Turin (2024) - 9005 series
	if strings.Contains(modelUpper, "EPYC") {
		info.Family = "AMD EPYC"

		// Match EPYC model numbers
		epycPattern := regexp.MustCompile(`(\d)(\d)(\d)\d`)
		if matches := epycPattern.FindStringSubmatch(modelUpper); len(matches) > 3 {
			series, _ := strconv.Atoi(matches[1])
			gen, _ := strconv.Atoi(matches[2])

			if series == 7 {
				// 7xxx series
				switch gen {
				case 0: // 70xx - 1st gen (Naples)
					info.Generation = 1
					info.Priority = 210
					info.ReleaseYear = 2017
				case 2: // 72xx - 2nd gen (Rome)
					info.Generation = 2
					info.Priority = 310
					info.ReleaseYear = 2019
				case 3: // 73xx - 3rd gen (Milan)
					info.Generation = 3
					info.Priority = 410
					info.ReleaseYear = 2021
				case 4: // 74xx - 3rd gen (Milan)
					info.Generation = 3
					info.Priority = 410
					info.ReleaseYear = 2021
				case 5: // 75xx - 3rd gen (Milan)
					info.Generation = 3
					info.Priority = 410
					info.ReleaseYear = 2021
				case 6: // 76xx - 3rd gen (Milan)
					info.Generation = 3
					info.Priority = 410
					info.ReleaseYear = 2021
				case 7: // 77xx - 3rd gen (Milan)
					info.Generation = 3
					info.Priority = 410
					info.ReleaseYear = 2021
				default:
					info.Priority = 200
				}
			} else if series == 9 {
				// 9xxx series - 4th gen (Genoa) and 5th gen (Turin)
				switch gen {
				case 0: // 90xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				case 1: // 91xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				case 2: // 92xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				case 3: // 93xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				case 4: // 94xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				case 5: // 95xx - 5th gen (Turin)
					info.Generation = 5
					info.Priority = 610
					info.ReleaseYear = 2024
				case 6: // 96xx - 4th gen (Genoa)
					info.Generation = 4
					info.Priority = 510
					info.ReleaseYear = 2022
				default:
					info.Priority = 500
				}
			}
			return info
		}
	}

	// AMD Ryzen (desktop/workstation)
	if strings.Contains(modelUpper, "RYZEN") {
		info.Family = "AMD Ryzen"
		// Ryzen 9/7/5/3 - extract generation from first digit of model number
		ryzenPattern := regexp.MustCompile(`RYZEN\s*[9753]\s*(\d)`)
		if matches := ryzenPattern.FindStringSubmatch(modelUpper); len(matches) > 1 {
			gen, _ := strconv.Atoi(matches[1])
			info.Generation = gen
			info.Priority = 100 + gen*20
			info.ReleaseYear = 2016 + gen
		}
		return info
	}

	// Intel Core (desktop/workstation)
	if strings.Contains(modelUpper, "CORE") {
		info.Family = "Intel Core"
		// Try to extract generation (i7-10700, i9-12900, etc.)
		corePattern := regexp.MustCompile(`I[9753]-(\d{1,2})`)
		if matches := corePattern.FindStringSubmatch(modelUpper); len(matches) > 1 {
			gen, _ := strconv.Atoi(matches[1])
			info.Generation = gen
			info.Priority = 90 + gen*10
			info.ReleaseYear = 2008 + gen
		}
		return info
	}

	// Try to extract any 4-digit model number as a rough priority
	genericPattern := regexp.MustCompile(`(\d{4})`)
	if matches := genericPattern.FindStringSubmatch(model); len(matches) > 1 {
		modelNum, _ := strconv.Atoi(matches[1])
		// Use the model number as a rough priority indicator
		info.Priority = modelNum / 10
	}

	return info
}

// shortenModel creates a short display name for a CPU model
func shortenModel(model string) string {
	replacements := []struct {
		old string
		new string
	}{
		{"Intel(R) Xeon(R) CPU ", "Xeon "},
		{"Intel(R) Xeon(R) ", "Xeon "},
		{"Intel(R) Core(TM) ", "Core "},
		{"AMD EPYC ", "EPYC "},
		{"AMD Ryzen ", "Ryzen "},
		{" Processor", ""},
		{" CPU", ""},
		{" @ ", " "},
		{"  ", " "},
	}

	result := model
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return strings.TrimSpace(result)
}

// GetCPUPriorityScore returns a normalized score (0-100) based on cluster CPU range
func GetCPUPriorityScore(cpuModel string, clusterInfo CPUPriorityInfo) float64 {
	if clusterInfo.MaxPriority == clusterInfo.MinPriority {
		return 50.0 // All CPUs have same priority
	}

	priority := GetCPUPriority(cpuModel)
	// Normalize to 0-100 range
	score := float64(priority-clusterInfo.MinPriority) / float64(clusterInfo.MaxPriority-clusterInfo.MinPriority) * 100
	return score
}

// GetCPUGenerationDescription returns a human-readable description of CPU generation
func GetCPUGenerationDescription(info CPUPriority) string {
	if info.ReleaseYear > 0 {
		return info.ShortName + " (Gen " + strconv.Itoa(info.Generation) + ", ~" + strconv.Itoa(info.ReleaseYear) + ")"
	}
	if info.Generation > 0 {
		return info.ShortName + " (Gen " + strconv.Itoa(info.Generation) + ")"
	}
	return info.ShortName
}

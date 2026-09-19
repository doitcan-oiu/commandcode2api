package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"commandcode2api/internal/config"
)

const fingerprintSalt = "command-code:device-fingerprint:v1"

var fingerprintCPUs = []struct {
	model string
	cores int
}{
	{"12th Gen Intel(R) Core(TM) i7-12650H", 10}, {"12th Gen Intel(R) Core(TM) i5-12400F", 6}, {"12th Gen Intel(R) Core(TM) i9-12900K", 16},
	{"13th Gen Intel(R) Core(TM) i7-13700K", 16}, {"13th Gen Intel(R) Core(TM) i5-13600K", 14}, {"13th Gen Intel(R) Core(TM) i9-13900K", 24},
	{"Intel(R) Core(TM) Ultra 7 155H", 16}, {"Intel(R) Core(TM) Ultra 9 285H", 16}, {"Intel(R) Core(TM) i9-14900K", 24}, {"Intel(R) Core(TM) i7-14700K", 20},
	{"AMD Ryzen 7 7800X3D", 8}, {"AMD Ryzen 9 7950X", 16}, {"AMD Ryzen 5 7600", 6}, {"AMD Ryzen 9 7900X", 12}, {"AMD Ryzen 7 5800X3D", 8},
}

func generateFingerprint(apiKey string, cfg config.Config) M {
	digest := func(field string) []byte {
		h := sha256.Sum256([]byte(cfg.FingerprintSalt + "\x00" + apiKey + "\x00" + field))
		return h[:]
	}
	pickIndex := func(field string, labels []string) int {
		best := 0
		var score []byte
		for i, label := range labels {
			next := digest(field + "\x00" + label)
			if score == nil || bytes.Compare(next, score) > 0 {
				best = i
				score = next
			}
		}
		return best
	}
	pick := func(field string, labels []string) string { return labels[pickIndex(field, labels)] }
	hexOf := func(field string, n int) string { return hex.EncodeToString(digest(field)[:n]) }
	labels := make([]string, len(fingerprintCPUs))
	for i, c := range fingerprintCPUs {
		labels[i] = fmt.Sprintf("%s|%d", c.model, c.cores)
	}
	cpu := fingerprintCPUs[pickIndex("cpu", labels)]
	mems := []int{8, 16, 24, 32, 48, 64}
	mem := mems[pickIndex("mem", []string{"8", "16", "24", "32", "48", "64"})]
	tz := pick("timezone", []string{"America/New_York", "America/Chicago", "America/Los_Angeles", "America/Toronto", "Europe/London", "Europe/Berlin", "Europe/Paris", "Europe/Moscow", "Asia/Shanghai", "Asia/Tokyo", "Asia/Singapore", "Asia/Seoul", "Asia/Hong_Kong", "Australia/Sydney", "Pacific/Auckland"})
	macCount := 2 + pickIndex("macCount", []string{"2", "3", "4", "5"})
	osUser := pick("osUser", []string{"dev", "user", "admin", "coder", "engineer", "work"})
	domain := pick("mailDomain", []string{"gmail.com", "outlook.com", "qq.com", "163.com"})
	mid := hexOf("machineId", 16)
	machineID := mid[:8] + "-" + mid[8:12] + "-" + mid[12:16] + "-" + mid[16:20] + "-" + mid[20:]
	macs := make([]string, macCount)
	for i := range macs {
		h := hexOf(fmt.Sprintf("mac%d", i), 6)
		macs[i] = h[:2] + ":" + h[2:4] + ":" + h[4:6] + ":" + h[6:8] + ":" + h[8:10] + ":" + h[10:]
	}
	sort.Strings(macs)
	hash := func(s string) string {
		h := sha256.Sum256([]byte(fingerprintSalt + "\x00" + strings.ToLower(strings.TrimSpace(s))))
		return hex.EncodeToString(h[:])
	}
	macHashes := make([]string, len(macs))
	for i, mac := range macs {
		macHashes[i] = hash(mac)
	}
	thumb := sha256.Sum256([]byte(fingerprintSalt + "\x00machine\x00" + machineID + "|" + strings.Join(macs, ",")))
	return M{"thumbmark": hex.EncodeToString(thumb[:]), "components": M{"machineIdHash": hash(machineID), "macHashes": macHashes, "osUserHash": hash(osUser), "hostnameHash": hash("DESKTOP-" + strings.ToUpper(hexOf("hostname", 4))), "gitEmailHash": hash(osUser + "." + hexOf("gitEmail", 3) + "@" + domain), "platform": "win32", "arch": "x64", "osRelease": "10.0.22631", "cpuModel": cpu.model, "cpuCount": cpu.cores, "memGiB": mem, "isContainer": false, "timezone": tz, "runtime": "cli", "collectorVersion": 1}}
}

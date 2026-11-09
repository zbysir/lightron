package pack

import (
	"image"
	"io"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// WindowsResources writes a .syso the Go linker puts into the .exe (it picks up *.syso of the package it builds):
// the icon Explorer and the taskbar show, a manifest (per-monitor v2 DPI, common controls v6), version information.
func WindowsResources(w io.Writer, arch string, icon image.Image, name, ver, company string) error {
	rs := winres.ResourceSet{}
	if icon != nil {
		ico, err := winres.NewIconFromResizedImage(icon, []int{16, 24, 32, 48, 64, 128, 256})
		if err != nil {
			return err
		}
		if err := rs.SetIcon(winres.Name("APP"), ico); err != nil {
			return err
		}
	}
	rs.SetManifest(winres.AppManifest{
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		ExecutionLevel:      winres.AsInvoker,
	})
	vi := version.Info{}
	v4 := fourPart(ver)
	vi.SetFileVersion(v4)
	vi.SetProductVersion(v4)
	_ = vi.Set(version.LangDefault, version.ProductName, name)
	_ = vi.Set(version.LangDefault, version.FileDescription, name)
	_ = vi.Set(version.LangDefault, version.ProductVersion, ver)
	if company != "" {
		_ = vi.Set(version.LangDefault, version.CompanyName, company)
	}
	rs.SetVersionInfo(vi)
	a := winres.ArchAMD64
	if arch == "arm64" {
		a = winres.ArchARM64
	}
	return rs.WriteObject(w, a)
}

// fourPart: a version as Windows wants it, a.b.c.d (git describe's commits past the tag as d).
func fourPart(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' })
	var nums []string
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			nums = append(nums, p)
		}
		if len(nums) == 4 {
			break
		}
	}
	for len(nums) < 4 {
		nums = append(nums, "0")
	}
	return strings.Join(nums, ".")
}

package store

import (
	"errors"
	"github.com/PMExtra/RedApp/presets"
	"strings"
)

type EntityTemplate struct {
	Vendor       VendorInput      `json:"vendor"`
	Application  ApplicationInput `json:"application"`
	Instructions LocalizedText    `json:"instructions"`
}

func EntityTemplates() []EntityTemplate {
	set := presets.Embedded()
	vendors := map[string]VendorInput{}
	for _, v := range set.Vendors {
		vendors[v.Metadata.ID] = vendorTemplate(v)
	}
	out := []EntityTemplate{}
	for _, a := range set.Apps {
		icon, _ := presets.Icon(a.Spec.Icon)
		out = append(out, EntityTemplate{Vendor: vendors[a.Metadata.Vendor], Application: ApplicationInput{ID: a.Metadata.ID, Name: LocalizedText{En: a.Spec.Name.En, ZhCN: a.Spec.Name.ZhCN}, Description: LocalizedText{En: a.Spec.Description.En, ZhCN: a.Spec.Description.ZhCN}, Icon: icon, Provider: a.Spec.Provider, BaseURL: a.Spec.BaseURL, BaseURLs: a.Spec.BaseURLs, SourceStrategy: a.Spec.SourceStrategy, CacheTTLSeconds: a.Spec.CacheTTLSeconds}, Instructions: LocalizedText{En: a.Spec.Instructions.En, ZhCN: a.Spec.Instructions.ZhCN}})
	}
	return out
}

func vendorTemplate(v presets.Vendor) VendorInput {
	icon, _ := presets.Icon(v.Spec.Icon)
	en, _ := presets.Icon(v.Spec.LocalizedIcons.En)
	zh, _ := presets.Icon(v.Spec.LocalizedIcons.ZhCN)
	return VendorInput{ID: v.Metadata.ID, Name: LocalizedText{En: v.Spec.Name.En, ZhCN: v.Spec.Name.ZhCN}, Description: LocalizedText{En: v.Spec.Description.En, ZhCN: v.Spec.Description.ZhCN}, Icon: icon, LocalizedIcons: LocalizedText{En: en, ZhCN: zh}}
}
func BuiltinApplicationTemplate(key string) (EntityTemplate, bool) {
	for _, v := range EntityTemplates() {
		if v.Vendor.ID+"/"+v.Application.ID == key {
			return v, true
		}
	}
	return EntityTemplate{}, false
}

var ErrBuiltinTemplate = errors.New("Applications matching a built-in template cannot be deleted; disable them instead")

func (s *Store) EnsureEntityTemplates() error { return s.ReconcileTemplates(presets.Embedded()) }

type TemplateReset struct {
	Revision             int64    `json:"revision"`
	InstructionsRevision int64    `json:"instructions_revision"`
	Groups               []string `json:"groups"`
}

func (s *Store) ResetApplicationTemplate(key string, in TemplateReset) (Application, error) {
	fields := map[string][]string{"metadata": {"name.en", "name.zh-CN", "description.en", "description.zh-CN"}, "icon": {"icon"}, "instructions_en": {"instructions.en"}, "instructions_zh": {"instructions.zh-CN"}, "source": {"base_url", "base_urls", "source_strategy"}, "cache": {"cache_ttl_seconds"}}
	unset, err := resetFields(in.Groups, fields)
	if err != nil {
		return Application{}, err
	}
	var expected *int64
	for _, p := range unset {
		if strings.HasPrefix(p, "instructions.") {
			expected = &in.InstructionsRevision
		}
	}
	if err = s.patchConfiguration("App", key, ConfigurationPatch{Revision: in.Revision, Unset: unset}, nil, expected); err != nil {
		return Application{}, err
	}
	return s.Application(key)
}
func resetFields(groups []string, fields map[string][]string) ([]string, error) {
	if len(groups) == 0 {
		return nil, ErrInvalidDirectory
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		p, ok := fields[g]
		if !ok || seen[g] {
			return nil, ErrInvalidDirectory
		}
		seen[g] = true
		out = append(out, p...)
	}
	return out, nil
}

func BuiltinVendorTemplate(id string) (VendorInput, bool) {
	for _, value := range presets.Embedded().Vendors {
		if value.Metadata.ID == id {
			return vendorTemplate(value), true
		}
	}
	return VendorInput{}, false
}
func (s *Store) ResetVendorTemplate(id string, in TemplateReset) (Vendor, error) {
	unset, err := resetFields(in.Groups, map[string][]string{"metadata": {"name.en", "name.zh-CN", "description.en", "description.zh-CN"}, "icon": {"icon", "localized_icons.en", "localized_icons.zh-CN"}})
	if err != nil {
		return Vendor{}, err
	}
	if err = s.patchConfiguration("Vendor", id, ConfigurationPatch{Revision: in.Revision, Unset: unset}, nil, nil); err != nil {
		return Vendor{}, err
	}
	return s.Vendor(id)
}

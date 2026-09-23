// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
)

// Model is the Terraform transport model used by the parent dashboard
// resource. Its fields exactly match the schema available at each nesting
// level; nested Dashify transport types remain private to this package.
type Model struct {
	ID         types.String                     `tfsdk:"id"`
	Title      types.String                     `tfsdk:"title"`
	ControlBar *dashifyControlBarModel          `tfsdk:"control_bar"`
	Layout     *dashifyLayoutOptionsModel       `tfsdk:"layout"`
	Container  []dashifyDashboardContainerModel `tfsdk:"container"`
}

type observabilityDashboardModel = Model

type dashifyControlBarModel struct {
	TimeRange    *dashifyTimeRangeControlModel     `tfsdk:"time_range"`
	Density      *dashifyDensityControlModel       `tfsdk:"density"`
	PinnedFilter []dashifyPinnedFilterControlModel `tfsdk:"pinned_filter"`
	FilterSet    *dashifyFilterSetControlModel     `tfsdk:"filter_set"`
}

type dashifyTimeRangeControlModel struct {
	Label                types.String `tfsdk:"label"`
	Description          types.String `tfsdk:"description"`
	Hidden               types.Bool   `tfsdk:"hidden"`
	DefaultVariableValue types.String `tfsdk:"default_variable_value"`
}

type dashifyDensityControlModel struct {
	Label                types.String `tfsdk:"label"`
	Description          types.String `tfsdk:"description"`
	Hidden               types.Bool   `tfsdk:"hidden"`
	DefaultVariableValue types.Int64  `tfsdk:"default_variable_value"`
}

type dashifyPinnedFilterControlModel struct {
	VariableName               types.String   `tfsdk:"variable_name"`
	Label                      types.String   `tfsdk:"label"`
	Description                types.String   `tfsdk:"description"`
	Hidden                     types.Bool     `tfsdk:"hidden"`
	Key                        types.String   `tfsdk:"key"`
	DefaultVariableValue       []types.String `tfsdk:"default_variable_value"`
	SuggestedValues            []types.String `tfsdk:"suggested_values"`
	OnlySuggestPreferredValues types.Bool     `tfsdk:"only_suggest_preferred_values"`
	MatchMissing               types.Bool     `tfsdk:"match_missing"`
	Required                   types.Bool     `tfsdk:"required"`
	ApplicationMode            types.String   `tfsdk:"application_mode"`
}

type dashifyFilterSetControlModel struct {
	Label       types.String                 `tfsdk:"label"`
	Description types.String                 `tfsdk:"description"`
	Hidden      types.Bool                   `tfsdk:"hidden"`
	Filter      []dashifyFilterSetEntryModel `tfsdk:"filter"`
}

type dashifyFilterSetEntryModel struct {
	Key      types.String   `tfsdk:"key"`
	Values   []types.String `tfsdk:"values"`
	Negated  types.Bool     `tfsdk:"negated"`
	Disabled types.Bool     `tfsdk:"disabled"`
}

type dashifyLayoutModel struct {
	Absolute  types.Bool   `tfsdk:"absolute"`
	Width     types.String `tfsdk:"width"`
	Height    types.String `tfsdk:"height"`
	MinWidth  types.String `tfsdk:"min_width"`
	MaxWidth  types.String `tfsdk:"max_width"`
	MinHeight types.String `tfsdk:"min_height"`
	MaxHeight types.String `tfsdk:"max_height"`
	X         types.String `tfsdk:"x"`
	Y         types.String `tfsdk:"y"`
}

type dashifyLayoutOptionsModel struct {
	Gap      types.Float64               `tfsdk:"gap"`
	Step     types.Float64               `tfsdk:"step"`
	Defaults *dashifyLayoutDefaultsModel `tfsdk:"defaults"`
}

type dashifyLayoutDefaultsModel struct {
	Absolute  types.Bool   `tfsdk:"absolute"`
	Width     types.String `tfsdk:"width"`
	Height    types.String `tfsdk:"height"`
	MinWidth  types.String `tfsdk:"min_width"`
	MaxWidth  types.String `tfsdk:"max_width"`
	MinHeight types.String `tfsdk:"min_height"`
	MaxHeight types.String `tfsdk:"max_height"`
}

type dashifyTemplateModel struct {
	TemplateID types.String `tfsdk:"template_id"`
	Content    types.String `tfsdk:"content"`
}

type dashifySectionModel struct {
	Title       types.String                   `tfsdk:"title"`
	Collapse    types.Bool                     `tfsdk:"collapse"`
	Collapsible types.Bool                     `tfsdk:"collapsible"`
	Layout      *dashifyLayoutOptionsModel     `tfsdk:"layout"`
	Container   []dashifySectionContainerModel `tfsdk:"container"`
}

type dashifyGroupModel struct {
	Title      types.String                 `tfsdk:"title"`
	Headerless types.Bool                   `tfsdk:"headerless"`
	Layout     *dashifyLayoutOptionsModel   `tfsdk:"layout"`
	Container  []dashifyGroupContainerModel `tfsdk:"container"`
}

// dashifyContainer is the shared semantic model used after decoding and
// before encoding the level-specific Terraform transport models.
type dashifyContainer struct {
	Layout   *dashifyLayoutModel
	Template *dashifyTemplateModel
	Section  *dashifySection
	Group    *dashifyGroup
	Charts   []charts.Entry
}

type dashifySection struct {
	Title       types.String
	Collapse    types.Bool
	Collapsible types.Bool
	Layout      *dashifyLayoutOptionsModel
	Container   []dashifyContainer
}

type dashifyGroup struct {
	Title      types.String
	Headerless types.Bool
	Layout     *dashifyLayoutOptionsModel
	Container  []dashifyContainer
}

type dashifyContainerLevel uint8

const (
	dashifyDashboardContainerLevel dashifyContainerLevel = iota
	dashifySectionContainerLevel
	dashifyGroupContainerLevel
)

func dashifyContainersFromDashboardModels(models []dashifyDashboardContainerModel) []dashifyContainer {
	containers := make([]dashifyContainer, len(models))
	for i, model := range models {
		container := dashifyContainer{Layout: model.Layout, Template: model.Template, Charts: model.Entries()}
		if model.Section != nil {
			container.Section = &dashifySection{
				Title:       model.Section.Title,
				Collapse:    model.Section.Collapse,
				Collapsible: model.Section.Collapsible,
				Layout:      model.Section.Layout,
				Container:   dashifyContainersFromSectionModels(model.Section.Container),
			}
		}
		if model.Group != nil {
			container.Group = dashifyGroupFromModel(model.Group)
		}
		containers[i] = container
	}
	return containers
}

func dashifyContainersFromSectionModels(models []dashifySectionContainerModel) []dashifyContainer {
	containers := make([]dashifyContainer, len(models))
	for i, model := range models {
		container := dashifyContainer{Layout: model.Layout, Template: model.Template, Charts: model.Entries()}
		if model.Group != nil {
			container.Group = dashifyGroupFromModel(model.Group)
		}
		containers[i] = container
	}
	return containers
}

func dashifyGroupFromModel(model *dashifyGroupModel) *dashifyGroup {
	return &dashifyGroup{
		Title:      model.Title,
		Headerless: model.Headerless,
		Layout:     model.Layout,
		Container:  dashifyContainersFromGroupModels(model.Container),
	}
}

func dashifyContainersFromGroupModels(models []dashifyGroupContainerModel) []dashifyContainer {
	containers := make([]dashifyContainer, len(models))
	for i, model := range models {
		containers[i] = dashifyContainer{Layout: model.Layout, Template: model.Template, Charts: model.Entries()}
	}
	return containers
}

func dashifyDashboardModelsFromContainers(containers []dashifyContainer) ([]dashifyDashboardContainerModel, error) {
	models := make([]dashifyDashboardContainerModel, len(containers))
	for i, container := range containers {
		model := dashifyDashboardContainerModel{Layout: container.Layout, Template: container.Template}
		if err := model.SetEntries(container.Charts); err != nil {
			return nil, fmt.Errorf("dashboard container %d charts: %w", i, err)
		}
		if container.Section != nil {
			sectionContainers, err := dashifySectionModelsFromContainers(container.Section.Container)
			if err != nil {
				return nil, fmt.Errorf("dashboard container %d section: %w", i, err)
			}
			model.Section = &dashifySectionModel{
				Title:       container.Section.Title,
				Collapse:    container.Section.Collapse,
				Collapsible: container.Section.Collapsible,
				Layout:      container.Section.Layout,
				Container:   sectionContainers,
			}
		}
		if container.Group != nil {
			group, err := dashifyGroupModelFromGroup(container.Group)
			if err != nil {
				return nil, fmt.Errorf("dashboard container %d group: %w", i, err)
			}
			model.Group = group
		}
		models[i] = model
	}
	return models, nil
}

func dashifySectionModelsFromContainers(containers []dashifyContainer) ([]dashifySectionContainerModel, error) {
	models := make([]dashifySectionContainerModel, len(containers))
	for i, container := range containers {
		model := dashifySectionContainerModel{Layout: container.Layout, Template: container.Template}
		if err := model.SetEntries(container.Charts); err != nil {
			return nil, fmt.Errorf("section container %d charts: %w", i, err)
		}
		if container.Group != nil {
			group, err := dashifyGroupModelFromGroup(container.Group)
			if err != nil {
				return nil, fmt.Errorf("section container %d group: %w", i, err)
			}
			model.Group = group
		}
		models[i] = model
	}
	return models, nil
}

func dashifyGroupModelFromGroup(group *dashifyGroup) (*dashifyGroupModel, error) {
	containers, err := dashifyGroupModelsFromContainers(group.Container)
	if err != nil {
		return nil, err
	}
	return &dashifyGroupModel{
		Title:      group.Title,
		Headerless: group.Headerless,
		Layout:     group.Layout,
		Container:  containers,
	}, nil
}

func dashifyGroupModelsFromContainers(containers []dashifyContainer) ([]dashifyGroupContainerModel, error) {
	models := make([]dashifyGroupContainerModel, len(containers))
	for i, container := range containers {
		model := dashifyGroupContainerModel{Layout: container.Layout, Template: container.Template}
		if err := model.SetEntries(container.Charts); err != nil {
			return nil, fmt.Errorf("group container %d charts: %w", i, err)
		}
		models[i] = model
	}
	return models, nil
}

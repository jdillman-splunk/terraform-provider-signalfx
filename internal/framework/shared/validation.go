// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwshared

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NonEmptyStringListValidators returns validators for string-list attributes
// whose configured elements must be non-null and non-empty.
func NonEmptyStringListValidators() []validator.List {
	return []validator.List{
		listvalidator.NoNullValues(),
		listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
	}
}

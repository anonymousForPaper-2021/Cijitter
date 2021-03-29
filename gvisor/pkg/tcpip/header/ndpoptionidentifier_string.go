// Copyright 2020 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by "stringer -type NDPOptionIdentifier ."; DO NOT EDIT.

package header

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NDPSourceLinkLayerAddressOptionType-1]
	_ = x[NDPTargetLinkLayerAddressOptionType-2]
	_ = x[NDPPrefixInformationType-3]
	_ = x[NDPRecursiveDNSServerOptionType-25]
}

const (
	_NDPOptionIdentifier_name_0 = "NDPSourceLinkLayerAddressOptionTypeNDPTargetLinkLayerAddressOptionTypeNDPPrefixInformationType"
	_NDPOptionIdentifier_name_1 = "NDPRecursiveDNSServerOptionType"
)

var (
	_NDPOptionIdentifier_index_0 = [...]uint8{0, 35, 70, 94}
)

func (i NDPOptionIdentifier) String() string {
	switch {
	case 1 <= i && i <= 3:
		i -= 1
		return _NDPOptionIdentifier_name_0[_NDPOptionIdentifier_index_0[i]:_NDPOptionIdentifier_index_0[i+1]]
	case i == 25:
		return _NDPOptionIdentifier_name_1
	default:
		return "NDPOptionIdentifier(" + strconv.FormatInt(int64(i), 10) + ")"
	}
}

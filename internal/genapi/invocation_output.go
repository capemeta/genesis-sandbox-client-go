package genapi

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var invocationOutputPath = regexp.MustCompile(`^output/[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validInvocationOutput(value *string) error {
	if value != nil && !invocationOutputPath.MatchString(*value) {
		return fmt.Errorf("invalid invocation output directory")
	}
	return nil
}

// 在已有请求编码入口预检，不修改旧大文件，也不把未授权路径发给服务。
func (request ExecSessionRequest) MarshalJSON() ([]byte, error) {
	if err := validInvocationOutput(request.OutputDirectory); err != nil {
		return nil, err
	}
	if request.SubprocessPolicy != nil && !request.SubprocessPolicy.Valid() {
		return nil, fmt.Errorf("invalid subprocess policy")
	}
	type plain ExecSessionRequest
	return json.Marshal(plain(request))
}

func (request AsyncExecRequest) MarshalJSON() ([]byte, error) {
	if request.TrustedGovernance != nil {
		if request.OperationId == nil || *request.OperationId == "" {
			return nil, fmt.Errorf("original governed operation ID required")
		}
		if err := request.TrustedGovernance.Validate(); err != nil {
			return nil, err
		}
	}
	if err := validInvocationOutput(request.OutputDirectory); err != nil {
		return nil, err
	}
	if request.SubprocessPolicy != nil && !request.SubprocessPolicy.Valid() {
		return nil, fmt.Errorf("invalid subprocess policy")
	}
	type plain AsyncExecRequest
	return json.Marshal(plain(request))
}

package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// 任意层重复身份字段均拒绝；整数精度仍由目标 DTO 的原生整数类型负责。
func validateJSONContract(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("response JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("response JSON nesting exceeds budget")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, nested := token.(json.Delim)
		if !nested {
			return nil
		}
		if delimiter == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, valid := key.(string)
				if !valid || seen[name] {
					return fmt.Errorf("response contains duplicate JSON fields")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		} else if delimiter == '[' {
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("response contains trailing JSON")
	}
	return nil
}

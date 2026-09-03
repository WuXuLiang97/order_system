package utils

import (
	"fmt"
	"html/template"
)

// FuncMap 返回自定义模板函数
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"money": func(currency string, amount float64) string {
			if currency == "" || currency == "CNY" {
				return fmt.Sprintf("¥%.2f", amount)
			}
			return fmt.Sprintf("%s %.2f", currency, amount)
		},
		"multiply": func(a, b interface{}) float64 {
			// 将 a 和 b 转为 float64
			var af, bf float64
			switch v := a.(type) {
			case int:
				af = float64(v)
			case float64:
				af = v
			default:
				af = 0
			}
			switch v := b.(type) {
			case int:
				bf = float64(v)
			case float64:
				bf = v
			default:
				bf = 0
			}
			return af * bf
		},
	}
}

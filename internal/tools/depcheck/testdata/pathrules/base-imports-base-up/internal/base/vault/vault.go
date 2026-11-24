package vault

// 故意違規的樣本：底層往上 import（vault 在 session 之下）。
import _ "github.com/minglight/evergenie/internal/base/session"

package sdk

import (
	"context"
	"time"
)

// NotesAPI 是全家共用的主題筆記（DEV-161，evergenie docs show schemas/note.md）的寫入：vault/notes/<分類>/<主題>.md。
// 任何秘書與主人都能寫；每一行內文的尾巴標是誰、哪天寫的（「（<職位> MM-DD）」，主人寫的標「主人」）。
// 讀（列出、看一份、找字）走 vault 的 get／list（種類 topic、topic_category）；兩層索引由核心照檔頭產生。
type NotesAPI interface {
	// Add 在一份主題的尾巴加一行，核心自動補上「（<寫的人> MM-DD）」；主題還沒有就建（要給分類與一句說明）。
	Add(ctx context.Context, req *NotesAddRequest) (*NotesAddResponse, error)
	// Set 整份取代一份主題的內文（整理用）及／或改檔頭；內文有沒標名的行就拒絕（主人寫的不檢查）。
	Set(ctx context.Context, req *NotesSetRequest) (*NotesSetResponse, error)
	// Archive 封存一份主題（搬進 archive/，不刪除；整理重複的主題用）。「主人」主題不能封存；帶 Into 時，被封存的主題
	// 如果有目標沒有的行就拒絕並列出行號與內容（請先用 Add 併進目標）。
	Archive(ctx context.Context, req *NotesArchiveRequest) (*NotesArchiveResponse, error)
	// AddCategory 加一個分類（資料夾＋說明檔）；已經有了回 CONFLICT。
	AddCategory(ctx context.Context, req *NotesAddCategoryRequest) (*NotesAddCategoryResponse, error)
	// Quips 回開場「梗本」那一節的全文（DEV-164；每位秘書都一樣）與條數：太長時只帶精選與最近的幾條，這週提過的先不帶。
	Quips(ctx context.Context, req *NotesQuipsRequest) (*NotesQuipsResponse, error)
	// QuipUsed 記下「剛剛回頭提了梗本裡的這一條」：之後 7 天開場不帶它（全家共用，誰提都算）。
	// Text 是那條梗裡的一段字；對到好幾條回 CONFLICT 並列出來，都對不到回 NOT_FOUND。
	QuipUsed(ctx context.Context, req *NotesQuipUsedRequest) (*NotesQuipUsedResponse, error)
}

type NotesAddRequest struct {
	Name     string   `json:"name,omitempty"`     // 主題名字或別名
	Line     string   `json:"line,omitempty"`     // 一行，不含換行
	Category string   `json:"category,omitempty"` // 新主題才用：放哪個分類（「主人」可省略，固定在「人」）
	Summary  string   `json:"summary,omitempty"`  // 新主題才用：一句說明
	Aliases  []string `json:"aliases,omitempty"`  // 新主題才用
	Follow   []string `json:"follow,omitempty"`   // 新主題才用：跟進的職位；沒給＝寫的秘書自己
	Links    []string `json:"links,omitempty"`    // 新主題才用：套件紀錄或別的主題，寫成 [[…]]
	By       *Actor   `json:"by,omitempty"`
}

type NotesAddResponse struct {
	Topic   *Record `json:"topic,omitempty"`
	Created bool    `json:"created"`        // 這次新建了主題
	Line    string  `json:"line,omitempty"` // 實際寫進去的那一行（含標記）
}

type NotesSetRequest struct {
	Name     string   `json:"name,omitempty"`
	Body     string   `json:"body,omitempty"`      // 新的整份內文（KeepBody 時不用）
	Revision string   `json:"revision,omitempty"`  // 讀到的那一版（notes show 的「版本」）；改內文一定要帶，版本對不上就拒絕（避免蓋掉別人剛寫的行）
	KeepBody bool     `json:"keep_body,omitempty"` // 只改檔頭、內文不動
	Summary  string   `json:"summary,omitempty"`   // 空＝不改
	Aliases  []string `json:"aliases,omitempty"`   // 沒給＝不改（要清空寫進 Clear）
	Follow   []string `json:"follow,omitempty"`
	Links    []string `json:"links,omitempty"`
	Clear    []string `json:"clear,omitempty"` // 要清空的檔頭清單欄位：aliases、follow、links
	By       *Actor   `json:"by,omitempty"`
}

type NotesSetResponse struct {
	Topic *Record `json:"topic,omitempty"`
}

type NotesArchiveRequest struct {
	Name string `json:"name,omitempty"` // 要封存的主題名字或別名
	Into string `json:"into,omitempty"` // 選用：要留下的那份（重複的主題併到它）；要存在，而且被封存的行它都要有
	By   *Actor `json:"by,omitempty"`
}

type NotesArchiveResponse struct {
	Name    string `json:"name,omitempty"`    // 封存掉的主題
	Path    string `json:"path,omitempty"`    // 封存後的檔案位置（相對於家）
	Into    string `json:"into,omitempty"`    // 併進哪一份（沒帶 Into 就是空的）
	Message string `json:"message,omitempty"` // 白話結果與怎麼找回
}

type NotesAddCategoryRequest struct {
	Name    string `json:"name,omitempty"`
	Summary string `json:"summary,omitempty"` // 一句說明：這個分類放什麼
	Body    string `json:"body,omitempty"`    // 說明檔內文（放什麼、誰常用）；空＝用一句說明
	By      *Actor `json:"by,omitempty"`
}

type NotesAddCategoryResponse struct {
	Category *Record `json:"category,omitempty"`
}

type NotesQuipsRequest struct {
	By *Actor `json:"by,omitempty"`
}

type NotesQuipsResponse struct {
	Text    string   `json:"text,omitempty"`    // 開場「梗本」那一節的全文
	Total   int32    `json:"total"`             // 梗本裡一共幾條
	Shown   int32    `json:"shown"`             // 開場帶了幾條
	Resting []string `json:"resting,omitempty"` // 這週提過、先不帶的（去掉行尾標記）
}

type NotesQuipUsedRequest struct {
	Text string `json:"text,omitempty"` // 那條梗裡的一段字（例：「」裡的那句）
	By   *Actor `json:"by,omitempty"`
}

type NotesQuipUsedResponse struct {
	Quip    string    `json:"quip,omitempty"`    // 對到的那一條（去掉行尾標記）
	Until   time.Time `json:"until"`             // 這個時間以前開場不帶它
	Message string    `json:"message,omitempty"` // 白話結果
}

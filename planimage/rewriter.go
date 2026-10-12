package planimage

import "github.com/jackc/pgx/v5/pgproto3"

// 計画でない結果をエラーにしても、バックエンドは後ろの文を実行してコミットする。
// 止めるにはキャンセルを送るしかなく、文が先に終わると効かないので、割り切っている
type Rewriter struct {
	pi    *PlanImage
	query string
	rows  *pendingRows
	// 同じ Query の2つ目以降の計画に、別の画像の名前を付けるため
	index int
}

type pendingRows struct {
	isPlan bool
	values []string
}

func (pi *PlanImage) NewRewriter(query string) *Rewriter {
	return &Rewriter{pi: pi, query: query}
}

func (r *Rewriter) Rewrite(msg pgproto3.BackendMessage) []pgproto3.BackendMessage {
	switch m := msg.(type) {
	case *pgproto3.RowDescription:
		// 行を返す結果は、終わりまでためてから判定する
		r.rows = &pendingRows{isPlan: len(m.Fields) == 1 && string(m.Fields[0].Name) == "QUERY PLAN"}
		return nil
	case *pgproto3.DataRow:
		if r.rows != nil {
			// string にしてコピーする(server.Rewriter の約束)
			for _, v := range m.Values {
				r.rows.values = append(r.rows.values, string(v))
			}
			return nil
		}
	case *pgproto3.CommandComplete:
		if r.rows != nil {
			reply := r.reply()
			r.rows = nil
			r.index++
			return reply
		}
	case *pgproto3.ErrorResponse:
		r.rows = nil
	}
	return []pgproto3.BackendMessage{msg}
}

func (r *Rewriter) reply() []pgproto3.BackendMessage {
	if !r.rows.isPlan || len(r.rows.values) != 1 {
		return []pgproto3.BackendMessage{planError(errNotJSONPlan)}
	}
	link, err := r.pi.Render(r.rows.values[0], r.query, r.index)
	if err != nil {
		return []pgproto3.BackendMessage{planError(err)}
	}
	return []pgproto3.BackendMessage{
		&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{
			Name:         []byte("plan"),
			DataTypeOID:  25, // text
			DataTypeSize: -1,
			TypeModifier: -1,
		}}},
		&pgproto3.DataRow{Values: [][]byte{[]byte(link)}},
		&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")},
	}
}

func planError(err error) *pgproto3.ErrorResponse {
	// feature_not_supported
	return &pgproto3.ErrorResponse{Severity: "ERROR", Code: "0A000", Message: err.Error()}
}

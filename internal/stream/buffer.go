package stream

import "time"

type Event struct {
	Time   time.Time `json:"ts"`
	Stream string    `json:"stream"`
	Text   string    `json:"text"`
}

type Summary struct {
	Total int     `json:"total"`
	Head  []Event `json:"head"`
	Tail  []Event `json:"tail"`
}

type Buffer struct {
	headCap int
	tailCap int
	total   int
	head    []Event
	tail    []Event
}

func NewBuffer(headCap, tailCap int) *Buffer {
	if headCap < 0 {
		headCap = 0
	}
	if tailCap < 0 {
		tailCap = 0
	}
	return &Buffer{
		headCap: headCap,
		tailCap: tailCap,
		head:    make([]Event, 0, headCap),
		tail:    make([]Event, 0, tailCap),
	}
}

func (b *Buffer) Add(e Event) {
	b.total++
	if len(b.head) < b.headCap {
		b.head = append(b.head, e)
	}
	if b.tailCap == 0 {
		return
	}
	if len(b.tail) < b.tailCap {
		b.tail = append(b.tail, e)
		return
	}
	copy(b.tail[0:], b.tail[1:])
	b.tail[b.tailCap-1] = e
}

func (b *Buffer) Snapshot() Summary {
	head := make([]Event, len(b.head))
	copy(head, b.head)
	tail := make([]Event, len(b.tail))
	copy(tail, b.tail)
	return Summary{
		Total: b.total,
		Head:  head,
		Tail:  tail,
	}
}

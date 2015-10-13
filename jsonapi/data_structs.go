package jsonapi

type document struct {
	Links    links  `json:"links,omitempty"`
	Data     data   `json:"data"`
	Included []data `json:"included,omitempty"`
}

type links struct {
	Self     string `json:"self,omitempty"`
	Related  string `json:"related,omitempty"`
	First    string `json:"first,omitempty"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
	Last     string `json:"last,omitempty"`
}

type data struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    map[string]interface{}  `json:"attributes"`
	Relationships map[string]relationship `json:"relationships,omitempty"`
	Links         links                   `json:"links,omitempty"`
}

type relationship struct {
	Links      links              `json:"links,omitempty"`
	DataToOne  relationshipData   `json:"data,omitempty"`
	DataToMany []relationshipData `json:"data,omitempty"`
}

type relationshipData struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

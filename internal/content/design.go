package content

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// designList lists the presentation imagery under web/static/img: pictures shown where a page
// has no lead image of its own. It is part of the design, not of a Content Release.
const designList = "img/credits.yml"

// designImage is one picture from web/static/img, keyed by the id of the page it illustrates.
type designImage struct {
	ID       string            `yaml:"id"`
	Src      string            `yaml:"src"`
	Variants []string          `yaml:"variants"`
	Alt      map[string]string `yaml:"alt"`
	Credit   string            `yaml:"credit"`
	Source   string            `yaml:"source"`
}

func (b *builder) loadDesign() error {
	raw, err := fs.ReadFile(b.opts.Static, designList)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("content: %s: %w", designList, err)
	}
	var list []designImage
	if err := decodeStrict(raw, &list); err != nil {
		return fmt.Errorf("content: %s: %w", designList, err)
	}
	b.design = map[string]designImage{}
	for _, d := range list {
		if b.assets["/static/"+d.Src] == nil {
			return fmt.Errorf("content: %s: %s: missing file %s", designList, d.ID, d.Src)
		}
		for _, l := range LocaleOrder() {
			if strings.TrimSpace(d.Alt[l]) == "" {
				return fmt.Errorf("content: %s: %s: no %s alt text", designList, d.ID, l)
			}
		}
		b.design[d.ID] = d
	}
	return nil
}

// pageImage is the page's lead image, else its design picture, else nil.
func (b *builder) pageImage(p *Page) *imageView {
	if p.Image != nil {
		return b.imageAt("/images/"+strings.TrimPrefix(p.Image.Src, "images/"), p.Image.Alt, p.Image.Credit)
	}
	if d, ok := b.design[p.ID]; ok {
		return b.imageAt("/static/"+d.Src, d.Alt[p.Locale], d.Credit)
	}
	return nil
}

// imageAt describes a served image with its intrinsic size and the -480/-960 variants next to it.
func (b *builder) imageAt(src, alt, credit string) *imageView {
	v := &imageView{Src: src, Alt: alt, Credit: credit}
	info, ok := b.images[src]
	if ok {
		v.Width, v.Height = info.width, info.height
	}
	ext := path.Ext(src)
	base := strings.TrimSuffix(src, ext)
	var set []string
	for _, w := range []int{480, 960} {
		variant := base + "-" + strconv.Itoa(w) + ext
		if b.assets[variant] != nil {
			set = append(set, variant+" "+strconv.Itoa(w)+"w")
		}
	}
	if len(set) > 0 && ok {
		set = append(set, src+" "+strconv.Itoa(info.width)+"w")
		v.Srcset = strings.Join(set, ", ")
	}
	return v
}

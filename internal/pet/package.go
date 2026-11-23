package pet

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

const MaxPackageBytes = 24 << 20

type Cell struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

type Frame struct {
	Cell
	MS int `json:"ms"`
}

type Animation struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Frames []Frame       `json:"frames"`
	Length time.Duration `json:"-"`
}

func (a *Animation) At(elapsed time.Duration) Cell {
	elapsed %= a.Length
	for _, frame := range a.Frames {
		elapsed -= time.Duration(frame.MS) * time.Millisecond
		if elapsed < 0 {
			return frame.Cell
		}
	}
	return a.Frames[0].Cell
}

type Idle struct {
	Animation string `json:"animation"`
	MinMS     int    `json:"minMs"`
	MaxMS     int    `json:"maxMs"`
}

type Look struct {
	Neutral    string `json:"neutral"`
	Anchor     [2]int `json:"anchor"`
	Directions []Cell `json:"directions"`
}

type Walk struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type Manifest struct {
	Version    int         `json:"version"`
	Name       string      `json:"name"`
	Atlas      string      `json:"atlas"`
	CellWidth  int         `json:"cellWidth"`
	CellHeight int         `json:"cellHeight"`
	Animations []Animation `json:"animations"`
	Idle       []Idle      `json:"idle"`
	Look       *Look       `json:"look,omitempty"`
	Walk       *Walk       `json:"walk,omitempty"`
	Jump       string      `json:"jump,omitempty"`
}

type Package struct {
	Manifest
	AtlasImage image.Image
	Clips      map[string]*Animation
}

func ReadFile(filename string) (*Package, []byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxPackageBytes+1))
	if err != nil {
		return nil, nil, err
	}
	pkg, err := Decode(data)
	return pkg, data, err
}

func Decode(data []byte) (*Package, error) {
	if len(data) > MaxPackageBytes {
		return nil, errors.New("архив питомца превышает 24 МБ")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("ожидается архив .pet или .zip: %w", err)
	}
	if len(reader.File) > 32 {
		return nil, errors.New("слишком много файлов в пакете")
	}
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name != path.Base(file.Name) || strings.Contains(file.Name, "\\") {
			continue
		}
		if files[file.Name] != nil {
			return nil, fmt.Errorf("файл %q встречается дважды", file.Name)
		}
		files[file.Name] = file
	}
	content, err := readEntry(files["pet.json"], "pet.json", 128<<10)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("ошибка в pet.json: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("после pet.json есть лишние данные")
	}
	if manifest.Version != 1 || strings.TrimSpace(manifest.Name) == "" || len([]rune(manifest.Name)) > 64 {
		return nil, errors.New("в pet.json нужны version: 1 и имя длиной до 64 символов")
	}
	if manifest.CellWidth < 16 || manifest.CellWidth > 320 || manifest.CellHeight < 16 || manifest.CellHeight > 320 {
		return nil, errors.New("ширина и высота кадра должны быть от 16 до 320 пикселей")
	}
	if manifest.Atlas == "" || !strings.HasSuffix(strings.ToLower(manifest.Atlas), ".png") {
		return nil, errors.New("атлас питомца должен быть файлом PNG")
	}
	imageData, err := readEntry(files[manifest.Atlas], manifest.Atlas, MaxPackageBytes)
	if err != nil {
		return nil, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("ошибка PNG: %w", err)
	}
	if config.Width == 0 || config.Height == 0 || config.Width > 4096 || config.Height > 4096 ||
		config.Width%manifest.CellWidth != 0 || config.Height%manifest.CellHeight != 0 {
		return nil, errors.New("атлас должен помещаться в 4096×4096 и состоять из целых ячеек")
	}
	atlas, err := png.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения PNG: %w", err)
	}
	pkg := &Package{Manifest: manifest, AtlasImage: atlas, Clips: make(map[string]*Animation)}
	if err := pkg.validate(); err != nil {
		return nil, err
	}
	return pkg, nil
}

func readEntry(file *zip.File, name string, limit int64) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("в корне архива нет файла %q", name)
	}
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("файл %q слишком большой", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("не удалось прочитать %q", name)
	}
	return data, nil
}

func (p *Package) Rect(cell Cell) image.Rectangle {
	x, y := cell.Col*p.CellWidth, cell.Row*p.CellHeight
	return image.Rect(x, y, x+p.CellWidth, y+p.CellHeight)
}

func (p *Package) validate() error {
	if len(p.Animations) < 1 || len(p.Animations) > 32 || len(p.Idle) < 1 || len(p.Idle) > 12 {
		return errors.New("укажите 1–32 анимации и 1–12 поз покоя")
	}
	columns := p.AtlasImage.Bounds().Dx() / p.CellWidth
	rows := p.AtlasImage.Bounds().Dy() / p.CellHeight
	checked := make(map[Cell]bool)
	check := func(cell Cell) error {
		if cell.Col < 0 || cell.Col >= columns || cell.Row < 0 || cell.Row >= rows {
			return fmt.Errorf("кадр [%d, %d] за пределами атласа", cell.Col, cell.Row)
		}
		if checked[cell] {
			return nil
		}
		rect := p.Rect(cell)
		var visible, transparent bool
		for y := rect.Min.Y; y < rect.Max.Y && !(visible && transparent); y++ {
			for x := rect.Min.X; x < rect.Max.X && !(visible && transparent); x++ {
				_, _, _, alpha := p.AtlasImage.At(x, y).RGBA()
				visible = visible || alpha > 0
				transparent = transparent || alpha == 0
			}
		}
		if !visible || !transparent {
			return fmt.Errorf("кадр [%d, %d] пустой или без прозрачного фона", cell.Col, cell.Row)
		}
		checked[cell] = true
		return nil
	}
	for i := range p.Animations {
		clip := &p.Animations[i]
		if clip.ID == "" || p.Clips[clip.ID] != nil || len(clip.Frames) == 0 ||
			len(clip.Frames) > 64 || strings.TrimSpace(clip.Label) == "" || len([]rune(clip.Label)) > 64 {
			return fmt.Errorf("некорректная или повторная анимация %q", clip.ID)
		}
		for _, frame := range clip.Frames {
			if frame.MS < 40 || frame.MS > 30000 {
				return fmt.Errorf("длительность кадра %q должна быть от 40 до 30000 мс", clip.ID)
			}
			if err := check(frame.Cell); err != nil {
				return fmt.Errorf("%s: %w", clip.ID, err)
			}
			clip.Length += time.Duration(frame.MS) * time.Millisecond
		}
		p.Clips[clip.ID] = clip
	}
	idles := make(map[string]bool)
	for _, idle := range p.Idle {
		if p.Clips[idle.Animation] == nil || idles[idle.Animation] ||
			idle.MinMS < 500 || idle.MaxMS < idle.MinMS || idle.MaxMS > 300000 {
			return fmt.Errorf("некорректная или повторная поза покоя %q", idle.Animation)
		}
		idles[idle.Animation] = true
	}
	if p.Look != nil {
		look := p.Look
		n := len(look.Directions)
		a := look.Anchor
		if !idles[look.Neutral] || (n != 4 && n != 8 && n != 16) ||
			a[0] < 0 || a[0] >= p.CellWidth || a[1] < 0 || a[1] >= p.CellHeight {
			return errors.New("look требует позу neutral из idle, допустимый anchor и 4, 8 или 16 направлений")
		}
		for _, cell := range look.Directions {
			if err := check(cell); err != nil {
				return err
			}
		}
	}
	if p.Walk != nil && (p.Clips[p.Walk.Left] == nil || p.Clips[p.Walk.Right] == nil || p.Walk.Left == p.Walk.Right) {
		return errors.New("walk должен ссылаться на две разные анимации")
	}
	if p.Jump != "" && p.Clips[p.Jump] == nil {
		return errors.New("jump должен ссылаться на существующую анимацию")
	}
	return nil
}

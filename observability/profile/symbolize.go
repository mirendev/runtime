package profile

import (
	"debug/dwarf"
	"debug/elf"
	"fmt"
	"os"

	"github.com/ironpark/skiplist"
)

type Symbol struct {
	Function string
	FuncAddr uint64
	File     string
	Line     int
}

type FuncSymbol struct {
	Name   string
	Lo, Hi uint64

	File      string
	StartLine int
}

func (f *FuncSymbol) Contains(pc uint64) bool {
	return f.Lo <= pc && pc < f.Hi
}

type Symbolizer struct {
	path      string
	file      *os.File
	dwarfData *dwarf.Data

	funcs skiplist.SkipList[uint64, *FuncSymbol]

	cache map[uint64]*Symbol
}

func NewSymbolizer(path string) (*Symbolizer, error) {
	// Open the ELF file.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Parse the ELF file.
	elfFile, err := elf.NewFile(file)
	if err != nil {
		return nil, err
	}

	// Get the DWARF data.
	dwarfData, err := elfFile.DWARF()
	if err != nil {
		return nil, err
	}

	funcs := skiplist.New[uint64, *FuncSymbol](skiplist.NumberComparator)

	return &Symbolizer{
		path:      path,
		file:      file,
		funcs:     funcs,
		dwarfData: dwarfData,
	}, nil
}

var ErrFuncNotFound = fmt.Errorf("function not found")

func (s *Symbolizer) ResolveFunc(pc uint64) (*FuncSymbol, error) {
	if elem := s.funcs.Find(pc); elem != nil {
		if elem.Value.Contains(pc) {
			return elem.Value, nil
		}
	}

	fn, err := s.resolveFunc(pc)
	if err != nil {
		return nil, ErrFuncNotFound
	}

	s.funcs.Set(fn.Lo, fn)

	return fn, nil
}

func (s *Symbolizer) Symbolize(addr uint64) (*Symbol, error) {
	if s.cache == nil {
		s.cache = make(map[uint64]*Symbol)
	}

	if sym, ok := s.cache[addr]; ok {
		return sym, nil
	}

	sym, err := s.lookupSymbol(addr)
	if err != nil {
		return nil, err
	}

	s.cache[addr] = sym
	return sym, nil
}

func (s *Symbolizer) lookupSymbol(addr uint64) (*Symbol, error) {
	reader := s.dwarfData.Reader()

	var sym Symbol

	for {
		entry, err := reader.Next()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			break
		}

		//fmt.Printf("entry: %s (%b)\n", entry.Tag, entry.Children)

		if sym.Function == "" && entry.Tag == dwarf.TagSubprogram {
			name, faddr, err := resolveFuncName(entry, addr)
			if err == nil {
				sym.Function = name
				sym.FuncAddr = faddr
			}
		}

		// Locate the line table.
		if entry.Tag == dwarf.TagCompileUnit {
			lineReader, err := s.dwarfData.LineReader(entry)
			if err != nil {
				return nil, err
			}

			if lineReader != nil {
				f, l, err := locateFunctionName(lineReader, addr)
				if err == nil {
					sym.File = f
					sym.Line = l

					if sym.Function != "" {
						break
					}
				}
			}
		}
	}

	return &sym, nil
}

func (s *Symbolizer) funcSourcePos(addr uint64) (string, int, error) {
	reader := s.dwarfData.Reader()

	for {
		entry, err := reader.Next()
		if err != nil {
			return "", 0, err
		}

		if entry == nil {
			break
		}

		// Locate the line table.
		if entry.Tag == dwarf.TagCompileUnit {
			lineReader, err := s.dwarfData.LineReader(entry)
			if err != nil {
				return "", 0, err
			}

			if lineReader != nil {
				var entry dwarf.LineEntry

				err := lineReader.SeekPC(addr, &entry)
				if err != nil {
					continue
				}

				return entry.File.Name, entry.Line, nil
			}
		}
	}

	return "", 0, nil
}

func (s *Symbolizer) resolveFunc(addr uint64) (*FuncSymbol, error) {
	reader := s.dwarfData.Reader()

	var sym FuncSymbol

	for {
		entry, err := reader.Next()
		if err != nil {
			return nil, err
		}

		if entry == nil {
			break
		}

		if entry.Tag == dwarf.TagSubprogram {
			lowPC, okLow := entry.Val(dwarf.AttrLowpc).(uint64)
			highPC, okHigh := entry.Val(dwarf.AttrHighpc).(uint64)
			if okLow && okHigh && lowPC <= addr && addr < highPC {

				sym.Name = entry.Val(dwarf.AttrName).(string)
				sym.Lo = lowPC
				sym.Hi = highPC

				file, startLine, _ := s.funcSourcePos(lowPC)
				sym.File = file
				sym.StartLine = startLine

				return &sym, nil
			}
		}
	}

	return &sym, nil
}

func resolveFuncName(entry *dwarf.Entry, addr uint64) (string, uint64, error) {
	lowPC, okLow := entry.Val(dwarf.AttrLowpc).(uint64)
	highPC, okHigh := entry.Val(dwarf.AttrHighpc).(uint64)
	if okLow && okHigh && lowPC <= addr && addr < highPC {
		return entry.Val(dwarf.AttrName).(string), lowPC, nil
	}

	return "", 0, fmt.Errorf("not found")
}

func locateFunctionName(lineReader *dwarf.LineReader, addr uint64) (string, int, error) {
	var entry dwarf.LineEntry

	err := lineReader.SeekPC(addr, &entry)
	if err != nil {
		return "", 0, err
	}

	return entry.File.Name, entry.Line, nil
}

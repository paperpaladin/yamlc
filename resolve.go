package main

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Tag resolution. This is the half of the project the comparison tool is
// built on: the same scalar text, read by the 1.2 core schema and by the
// 1.1 rules, often gives two different values, and that is the thing worth
// reporting about a document.

type VKind uint8

const (
	VNull VKind = iota
	VBool
	VInt
	VFloat
	VStr
	VSeq
	VMap
	VStamp // 1.1 timestamp
	VBin   // !!binary
	VOther // a tag carried but not interpreted
)

type KV struct {
	Key, Val *Value
}

type Value struct {
	Kind  VKind
	Bool  bool
	Int   *big.Int
	Float float64
	Str   string
	Seq   []*Value
	Map   []KV
	Tag   string
	Line  int
	Col   int
}

const (
	tagStr   = "tag:yaml.org,2002:str"
	tagInt   = "tag:yaml.org,2002:int"
	tagFloat = "tag:yaml.org,2002:float"
	tagBool  = "tag:yaml.org,2002:bool"
	tagNull  = "tag:yaml.org,2002:null"
	tagSeq   = "tag:yaml.org,2002:seq"
	tagMap   = "tag:yaml.org,2002:map"
	tagBin   = "tag:yaml.org,2002:binary"
	tagStamp = "tag:yaml.org,2002:timestamp"
	tagMerge = "tag:yaml.org,2002:merge"
	tagValue = "tag:yaml.org,2002:value"
)

// The 1.2 core schema, section 10.2.1.2. Quoted from the table there:
//
//	null | Null | NULL | ~                          tag:yaml.org,2002:null
//	/* Empty */                                     tag:yaml.org,2002:null
//	true | True | TRUE | false | False | FALSE      tag:yaml.org,2002:bool
//	[-+]? [0-9]+                                    tag:yaml.org,2002:int
//	0o [0-7]+                                       tag:yaml.org,2002:int
//	0x [0-9a-fA-F]+                                 tag:yaml.org,2002:int
//	[-+]? ( \. [0-9]+ | [0-9]+ ( \. [0-9]* )? )
//	  ( [eE] [-+]? [0-9]+ )?                        tag:yaml.org,2002:float
//	[-+]? ( \.inf | \.Inf | \.INF )                 tag:yaml.org,2002:float
//	\.nan | \.NaN | \.NAN                           tag:yaml.org,2002:float
//	*                                               tag:yaml.org,2002:str
//
// Note what is NOT there: no octal with a leading zero, no sexagesimal, no
// timestamp, no yes or no, and no sign on the hex and octal forms.
var (
	re12Null  = regexp.MustCompile(`^(null|Null|NULL|~|)$`)
	re12Bool  = regexp.MustCompile(`^(true|True|TRUE|false|False|FALSE)$`)
	re12Dec   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	re12Oct   = regexp.MustCompile(`^0o[0-7]+$`)
	re12Hex   = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	re12Float = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
	re12Inf   = regexp.MustCompile(`^[-+]?\.(inf|Inf|INF)$`)
	re12NaN   = regexp.MustCompile(`^\.(nan|NaN|NAN)$`)
)

// The 1.1 rules, exactly as PyYAML 6.0.2 implements them in resolver.py.
// They are copied from that file rather than from the 1.1 specification,
// and the difference is recorded: the 1.1 type repository's bool also
// accepts a bare y, Y, n and N, and PyYAML's regex does not, so a document
// holding "y" is a string to PyYAML and true to a reader that follows the
// 1.1 text. yamlc reports PyYAML's reading, because PyYAML is the oracle
// this table is checked against.
//
// The order matters and is PyYAML's: bool, float, int, merge, null,
// timestamp, value. float before int is why "1.0" is a float.
var (
	re11Bool  = regexp.MustCompile(`^(yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	re11Float = regexp.MustCompile(`^([-+]?([0-9][0-9_]*)\.[0-9_]*([eE][-+][0-9]+)?` +
		`|\.[0-9][0-9_]*([eE][-+][0-9]+)?` +
		`|[-+]?[0-9][0-9_]*(:[0-5]?[0-9])+\.[0-9_]*` +
		`|[-+]?\.(inf|Inf|INF)` +
		`|\.(nan|NaN|NAN))$`)
	re11Int = regexp.MustCompile(`^([-+]?0b[0-1_]+` +
		`|[-+]?0[0-7_]+` +
		`|[-+]?(0|[1-9][0-9_]*)` +
		`|[-+]?0x[0-9a-fA-F_]+` +
		`|[-+]?[1-9][0-9_]*(:[0-5]?[0-9])+)$`)
	re11Null  = regexp.MustCompile(`^(~|null|Null|NULL|)$`)
	re11Stamp = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2}` +
		`|[0-9]{4}-[0-9][0-9]?-[0-9][0-9]?` +
		`([Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](\.[0-9]*)?` +
		`([ \t]*(Z|[-+][0-9][0-9]?(:[0-9][0-9])?))?)$`)
)

// resolveTag names the tag a plain scalar carries under one schema, without
// building a value. diverge.go reports these side by side.
func resolveTag(text string, schema int) string {
	if schema == 11 {
		switch {
		case re11Bool.MatchString(text):
			return tagBool
		case re11Float.MatchString(text):
			return tagFloat
		case re11Int.MatchString(text):
			return tagInt
		case text == "<<":
			return tagMerge
		case re11Null.MatchString(text):
			return tagNull
		case re11Stamp.MatchString(text):
			return tagStamp
		case text == "=":
			return tagValue
		}
		return tagStr
	}
	switch {
	case re12Null.MatchString(text):
		return tagNull
	case re12Bool.MatchString(text):
		return tagBool
	case re12Dec.MatchString(text), re12Oct.MatchString(text), re12Hex.MatchString(text):
		return tagInt
	case re12Float.MatchString(text), re12Inf.MatchString(text), re12NaN.MatchString(text):
		return tagFloat
	}
	return tagStr
}

// scalarValue builds the value for a scalar node under one schema. A
// refusal here is for an explicit tag whose content does not fit it:
// "!!int abc" is an error, not a string.
func (c *composer) scalarValue(n *Node) (*Value, error) {
	v := &Value{Line: n.Line, Col: n.Col}
	tag := n.Tag
	switch tag {
	case "", "?":
		if n.Style != StPlain {
			tag = tagStr
		} else {
			tag = resolveTag(n.Val, c.schema)
		}
	case "!":
		// Section 10.1.1: the non-specific tag on a scalar resolves to str.
		tag = tagStr
	}
	v.Tag = tag
	switch tag {
	case tagStr:
		v.Kind, v.Str = VStr, n.Val
		return v, nil
	case tagNull:
		if !c.nullOK(n.Val) {
			return nil, c.errAt(n, "%q is not a null", n.Val)
		}
		v.Kind = VNull
		return v, nil
	case tagBool:
		b, ok := c.boolOf(n.Val, explicitTag(n))
		if !ok {
			return nil, c.errAt(n, "%q is not a boolean", n.Val)
		}
		v.Kind, v.Bool = VBool, b
		return v, nil
	case tagInt:
		i, ok := c.intOf(n.Val, explicitTag(n))
		if !ok {
			return nil, c.errAt(n, "%q is not an integer", n.Val)
		}
		v.Kind, v.Int = VInt, i
		return v, nil
	case tagFloat:
		f, ok := c.floatOf(n.Val, explicitTag(n))
		if !ok {
			return nil, c.errAt(n, "%q is not a float", n.Val)
		}
		v.Kind, v.Float = VFloat, f
		return v, nil
	case tagStamp:
		s, ok := stampOf(n.Val)
		if !ok {
			return nil, c.errAt(n, "%q is not a timestamp", n.Val)
		}
		v.Kind, v.Str = VStamp, s
		return v, nil
	case tagBin:
		b, ok := base64Decode(n.Val)
		if !ok {
			return nil, c.errAt(n, "!!binary content is not base64")
		}
		v.Kind, v.Str = VBin, b
		return v, nil
	case tagMerge, tagValue:
		v.Kind, v.Str = VStr, n.Val
		return v, nil
	}
	v.Kind, v.Str = VOther, n.Val
	return v, nil
}

// explicitTag reports whether the node named its own type. It matters
// because PyYAML's constructors are much more permissive than its
// resolvers: "1" does not resolve to a float under the 1.1 rules, and
// !!float "1" still gives 1.0, because construct_yaml_float calls float()
// on whatever it is given. yamlc follows that under --schema=1.1, where
// PyYAML is the oracle, and uses the 1.2 forms under --schema=1.2, where
// it is not.
func explicitTag(n *Node) bool {
	switch n.Tag {
	case "", "?", "!":
		return false
	}
	return true
}

func (c *composer) nullOK(s string) bool {
	if c.schema == 11 {
		return re11Null.MatchString(s)
	}
	return re12Null.MatchString(s)
}

func (c *composer) boolOf(s string, explicit bool) (bool, bool) {
	if c.schema == 11 {
		if explicit {
			// construct_yaml_bool reads the 1.1 bool table, which is
			// wider than the resolver's and includes y, n, yes and off.
			switch strings.ToLower(s) {
			case "yes", "true", "on", "y":
				return true, true
			case "no", "false", "off", "n":
				return false, true
			}
			return false, false
		}
		if !re11Bool.MatchString(s) {
			return false, false
		}
		switch strings.ToLower(s) {
		case "yes", "true", "on":
			return true, true
		}
		return false, true
	}
	if !re12Bool.MatchString(s) {
		return false, false
	}
	return strings.ToLower(s) == "true", true
}

// intOf converts under the schema in force. 1.1 allows underscores, a
// leading zero for octal, 0b for binary and base-60 digits separated by
// colons, and 1.2's core schema allows none of those.
func (c *composer) intOf(s string, explicit bool) (*big.Int, bool) {
	if c.schema == 11 {
		if !explicit && !re11Int.MatchString(s) {
			return nil, false
		}
		if s == "" {
			return nil, false
		}
		t := strings.ReplaceAll(s, "_", "")
		neg := false
		if t[0] == '-' {
			neg = true
		}
		if t[0] == '-' || t[0] == '+' {
			t = t[1:]
		}
		var n *big.Int
		switch {
		case t == "0":
			n = big.NewInt(0)
		case strings.HasPrefix(t, "0b"):
			n = parseBase(t[2:], 2)
		case strings.HasPrefix(t, "0x"):
			n = parseBase(t[2:], 16)
		case t[0] == '0':
			n = parseBase(t, 8)
		case strings.Contains(t, ":"):
			n = parseSexagesimal(t)
		default:
			n = parseBase(t, 10)
		}
		if n == nil {
			return nil, false
		}
		if neg {
			n.Neg(n)
		}
		return n, true
	}
	switch {
	case re12Dec.MatchString(s):
		t := s
		if t[0] == '+' {
			t = t[1:]
		}
		n, ok := new(big.Int).SetString(t, 10)
		return n, ok
	case re12Oct.MatchString(s):
		return parseBase(s[2:], 8), true
	case re12Hex.MatchString(s):
		return parseBase(s[2:], 16), true
	}
	return nil, false
}

func parseBase(s string, base int) *big.Int {
	n, ok := new(big.Int).SetString(s, base)
	if !ok {
		return nil
	}
	return n
}

// parseSexagesimal reads base 60. 1:30 is ninety in 1.1 and the string
// "1:30" in 1.2, which is the divergence most likely to be in a real
// document: a port range, a time of day, a git refspec.
func parseSexagesimal(s string) *big.Int {
	total := new(big.Int)
	sixty := big.NewInt(60)
	for _, part := range strings.Split(s, ":") {
		d, ok := new(big.Int).SetString(part, 10)
		if !ok {
			return nil
		}
		total.Mul(total, sixty)
		total.Add(total, d)
	}
	return total
}

func (c *composer) floatOf(s string, explicit bool) (float64, bool) {
	if c.schema == 11 {
		if !explicit && !re11Float.MatchString(s) {
			return 0, false
		}
		if s == "" {
			return 0, false
		}
		t := strings.ToLower(strings.ReplaceAll(s, "_", ""))
		sign := 1.0
		if t[0] == '-' {
			sign = -1
		}
		if t[0] == '-' || t[0] == '+' {
			t = t[1:]
		}
		switch {
		case t == ".inf":
			return sign * math.Inf(1), true
		case t == ".nan":
			return math.NaN(), true
		case strings.Contains(t, ":"):
			parts := strings.Split(t, ":")
			total, base := 0.0, 1.0
			for i := len(parts) - 1; i >= 0; i-- {
				d, err := strconv.ParseFloat(parts[i], 64)
				if err != nil {
					return 0, false
				}
				total += d * base
				base *= 60
			}
			return sign * total, true
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		return sign * f, true
	}
	switch {
	case re12Inf.MatchString(s):
		if s[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case re12NaN.MatchString(s):
		return math.NaN(), true
	case re12Float.MatchString(s):
		f, err := strconv.ParseFloat(strings.TrimPrefix(s, "+"), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

var reStamp = regexp.MustCompile(`^([0-9]{4})-([0-9][0-9]?)-([0-9][0-9]?)` +
	`(?:(?:[Tt]|[ \t]+)([0-9][0-9]?):([0-9][0-9]):([0-9][0-9])(?:\.([0-9]*))?` +
	`(?:[ \t]*(Z|([-+])([0-9][0-9]?)(?::([0-9][0-9]))?))?)?$`)

// stampOf canonicalises a 1.1 timestamp the way PyYAML's constructor does,
// including its choice to keep the offset rather than normalise it. The
// form is what t/pyyaml.py prints for a date or a datetime, so the two can
// be compared as text.
func stampOf(s string) (string, bool) {
	m := reStamp.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	two := func(x string) string {
		if len(x) == 1 {
			return "0" + x
		}
		return x
	}
	// PyYAML builds a datetime.date here, so an impossible date raises
	// rather than resolving. The same document has to be a refusal for
	// yamlc, or the two readers disagree about whether it is a document at
	// all.
	y, _ := atoiAll(m[1])
	mo, _ := atoiAll(m[2])
	dy, _ := atoiAll(m[3])
	if !validDate(y, mo, dy) {
		return "", false
	}
	if m[4] != "" {
		h, _ := atoiAll(m[4])
		mi, _ := atoiAll(m[5])
		se, _ := atoiAll(m[6])
		if h > 23 || mi > 59 || se > 59 {
			return "", false
		}
		if m[10] != "" {
			tzh, _ := atoiAll(m[10])
			if tzh > 23 {
				return "", false
			}
		}
	}
	date := m[1] + "-" + two(m[2]) + "-" + two(m[3])
	if m[4] == "" {
		return "date " + date, true
	}
	frac := m[7]
	if len(frac) > 6 {
		frac = frac[:6]
	}
	for len(frac) < 6 {
		frac += "0"
	}
	out := "datetime " + date + " " + two(m[4]) + ":" + m[5] + ":" + m[6] + "." + frac
	switch {
	case m[8] == "Z":
		out += " +00:00"
	case m[9] != "":
		tzm := m[11]
		if tzm == "" {
			tzm = "00"
		}
		out += " " + m[9] + two(m[10]) + ":" + tzm
	}
	return out, true
}

func validDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	n := days[m-1]
	if m == 2 && (y%4 == 0 && (y%100 != 0 || y%400 == 0)) {
		n = 29
	}
	return d <= n
}

const b64chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// base64Decode is here because !!binary is a core tag of the 1.1 schema and
// PyYAML decodes it, so a comparison needs the bytes rather than the text.
func base64Decode(s string) (string, bool) {
	var out []byte
	var acc, bits int
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' || c == ' ' || c == '\t' || c == '\r' {
			continue
		}
		if c == '=' {
			break
		}
		j := strings.IndexByte(b64chars, c)
		if j < 0 {
			return "", false
		}
		acc = acc<<6 | j
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
		}
	}
	return string(out), true
}

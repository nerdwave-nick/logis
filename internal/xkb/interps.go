package xkb

// interps are the compat interprets in libxkbcommon's search order (most
// specific first: by keysym before Any, then by match kind), indexed by
// keysym so a key's lookup costs only the interprets that can match it.
type interps struct {
	sorted []interp
	bySym  map[Keysym][]int // indices into sorted, ascending
	anySym []int
	work   int // interprets tried so far
}

func sortInterps(all []interp) *interps {
	in := &interps{bySym: map[Keysym][]int{}}
	for _, needSym := range []bool{true, false} {
		for _, m := range []int{matchExactly, matchAll, matchNone, matchAny, matchAnyOrNone} {
			for _, x := range all {
				if x.match == m && !x.anySym == needSym {
					in.sorted = append(in.sorted, x)
				}
			}
		}
	}
	for i, x := range in.sorted {
		if x.anySym {
			in.anySym = append(in.anySym, i)
		} else {
			in.bySym[x.sym] = append(in.bySym[x.sym], i)
		}
	}
	return in
}

// matches says whether interpret x applies to a key with modifier map
// mods at level li.
func (x *interp) matches(mods Mask, li int) bool {
	if x.levelOneOnly && li != 0 {
		mods = 0
	}
	switch x.match {
	case matchNone:
		return x.mods&mods == 0
	case matchAnyOrNone:
		return mods == 0 || x.mods&mods != 0
	case matchAny:
		return x.mods&mods != 0
	case matchAll:
		return x.mods&mods == x.mods
	case matchExactly:
		return x.mods == mods
	}
	return false
}

// find is libxkbcommon's FindInterpForKey for one keysym of a level: the
// first interpret that matches, or nil for the default. An Any interpret
// already used for another keysym of the level (found) ends the search.
func (in *interps) find(sym Keysym, modmap Mask, li int, found []*interp) *interp {
	try := func(i int) (hit *interp, stop bool) {
		in.work++
		x := &in.sorted[i]
		if !x.matches(modmap, li) {
			return nil, false
		}
		if x.anySym && i > 0 {
			for _, prev := range found {
				if prev == x {
					return nil, true
				}
			}
		}
		return x, true
	}
	for _, i := range in.bySym[sym] {
		if hit, stop := try(i); stop {
			return hit
		}
	}
	for _, i := range in.anySym {
		if hit, stop := try(i); stop {
			return hit
		}
	}
	return nil
}

// apply sets key k's repeat flag and virtual modifier map from the
// interprets matching its keysyms (libxkbcommon's ApplyInterpsToKey). It
// refuses to go on once the interprets tried exceed maxWork.
func (in *interps) apply(k *key) error {
	var vmodmap Mask
	def := interp{repeat: true, vmod: -1}
	var found []*interp
	for gi, g := range k.groups {
		if g.explicitActions {
			continue
		}
		for li, syms := range g.levels {
			found = found[:0]
			for _, sym := range syms {
				hit := in.find(sym, k.modmap, li, found)
				if hit == nil {
					hit = &def
				}
				found = append(found, hit)
			}
			if in.work > maxWork {
				return errorf(0, "too complex: matching its interprets takes more than %d steps", maxWork)
			}
			for _, x := range found {
				if gi == 0 && li == 0 && !k.explicitRepeat && x.repeat {
					k.repeats = true
				}
				if (gi == 0 && li == 0) || !x.levelOneOnly {
					if x.vmod >= 0 {
						vmodmap |= Mask(1) << x.vmod
					}
				}
			}
		}
	}
	if !k.explicitVmodmap {
		k.vmodmap = vmodmap
	}
	return nil
}

package coverage

import (
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// slideSchemeColors are the scheme colour names a slide colour may take:
// a:ST_SchemeColorVal from the generated schema tables, without phClr.
func slideSchemeColors() []string {
	var out []string
	for _, v := range edit.Enum("a:ST_SchemeColorVal") {
		if v != "phClr" {
			out = append(out, v)
		}
	}
	return out
}

// Path fragments used by the registry.
const (
	colors = "{a:srgbClr,a:schemeClr,a:sysClr,a:prstClr,a:hslClr,a:scrgbClr}"
	// spPrOwners are the objects with p:spPr.
	spPrOwners = "{p:sp,p:cxnSp,p:pic}"
	// nvProps are the objects' non-visual property elements.
	nvProps = "{p:sp/p:nvSpPr,p:cxnSp/p:nvCxnSpPr,p:grpSp/p:nvGrpSpPr,p:pic/p:nvPicPr,p:graphicFrame/p:nvGraphicFramePr}"
	// colorBodies are the factored colour types.
	colorBodies = "{(a:CT_SRgbColor),(a:CT_SchemeColor),(a:CT_SystemColor),(a:CT_PresetColor),(a:CT_HslColor),(a:CT_ScRgbColor)}"
	// levels are a list style's level elements.
	levels = "(a:CT_TextListStyle)/{a:defPPr,a:lvl1pPr,a:lvl2pPr,a:lvl3pPr,a:lvl4pPr,a:lvl5pPr,a:lvl6pPr,a:lvl7pPr,a:lvl8pPr,a:lvl9pPr}"
	// slots are a colour scheme's slots.
	slots = "{a:dk1,a:lt1,a:dk2,a:lt2,a:accent1,a:accent2,a:accent3,a:accent4,a:accent5,a:accent6,a:hlink,a:folHlink}"
	// runProps are the character properties the HTML of a run shows.
	runProps = "p:sp/p:txBody/a:p/{a:r,a:fld}/a:rPr"
	// paraProps are a paragraph's properties.
	paraProps = "p:sp/p:txBody/a:p/a:pPr"
	// bodyAttrs are the a:bodyPr attributes the body codec reads and
	// writes (derive.BodyAttrs).
	bodyAttrs = "p:sp/p:txBody/a:bodyPr/@{lIns,tIns,rIns,bIns,anchor,wrap,vert,rot,numCol,spcCol,rtlCol,anchorCtr,upright,vertOverflow,horzOverflow,compatLnSpc,spcFirstLastPara,fromWordArt,forceAA}"
	// autofit is a:bodyPr's autofit choice.
	autofit = "p:sp/p:txBody/a:bodyPr/{a:noAutofit,a:normAutofit,a:spAutoFit}"
	// textParaPaths are the paragraph properties the HTML of a paragraph
	// shows beyond alignment, level and bullet choice.
	textParaPaths = paraProps + "/{@marL,@marR,@indent,@defTabSz,@rtl,a:lnSpc,a:spcBef,a:spcAft,a:buClrTx,a:buClr,a:buSzTx,a:buSzPct,a:buSzPts,a:buFontTx,a:buFont,a:tabLst}"
)

// Claims is the registry: what each reader and codec honours, checked
// against behaviour by this package's tests (see Claim). Order matters
// within a dimension: the first matching claim decides.
var Claims = []*Claim{
	// --- Read: the read model (internal/pptx/model) and the derive calls it
	// shares with the codecs. ---
	{By: "model.object (cNvPr, derive.ReadPh)", Dim: Read, Status: ReadModelled, Paths: []string{
		nvProps, nvProps + "/p:cNvPr", nvProps + "/p:cNvPr/@{id,name,hidden,descr,title}",
		"p:sp/p:nvSpPr/p:cNvSpPr", "p:sp/p:nvSpPr/p:cNvSpPr/@txBox",
		nvProps + "/p:nvPr", nvProps + "/p:nvPr/p:ph", nvProps + "/p:nvPr/p:ph/@{type,idx}",
	}},
	{By: "derive.ReadTransform (transform codec's read half)", Dim: Read, Status: ReadModelled, Paths: []string{
		spPrOwners + "/p:spPr", spPrOwners + "/p:spPr/a:xfrm/**",
		"p:grpSp/p:grpSpPr", "p:grpSp/p:grpSpPr/a:xfrm/**",
		"p:graphicFrame/p:xfrm/**",
	}},
	{By: "derive.ReadGeom (geometry codec's read half)", Dim: Read, Status: ReadModelled, Paths: []string{
		spPrOwners + "/p:spPr/a:prstGeom/**",
	}},
	{By: "model.geometry, derive.CustomPaths and ShowCustom", Dim: Read, Status: OmittedWithReason, Paths: []string{
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst/a:path/a:arcTo",
	}, Note: "arcTo omits the path (geom.path): the SVG subset has no counterpart (design: Paths)"},
	{By: "model.geometry, derive.CustomPaths and ShowCustom", Dim: Read, Status: ReadModelled, Paths: []string{
		spPrOwners + "/p:spPr/a:custGeom",
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst",
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst/a:path",
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst/a:path/@{w,h,fill,stroke}",
		spPrOwners + "/p:spPr/a:custGeom/{a:avLst,a:gdLst}", spPrOwners + "/p:spPr/a:custGeom/{a:avLst,a:gdLst}/a:gd",
		spPrOwners + "/p:spPr/a:custGeom/{a:avLst,a:gdLst}/a:gd/@{name,fmla}",
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo,a:close}/**",
		spPrOwners + "/p:spPr/a:custGeom/a:ahLst/**",
		spPrOwners + "/p:spPr/a:custGeom/a:cxnLst/**",
		spPrOwners + "/p:spPr/a:custGeom/a:rect/**",
	}, Note: "path shows the paths (an array with fill and stroke modes when there are several or they differ from the defaults); --detail full shows custom (av, gd, ah as written; cxn and rect with literal coordinates scaled like the path)"},
	{By: "derive.ColorCtx.ReadFill (fill codec's read half)", Dim: Read, Status: OmittedWithReason, Paths: []string{
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/a:grpFill", "p:cxnSp/p:spPr/a:grpFill",
	}, Note: "a:grpFill without a derived enclosing group fill (a connector's fill is read from its own spPr only)"},
	{By: "derive.ColorCtx.ReadFill (fill codec's read half)", Dim: Read, Status: ReadModelled, Paths: []string{
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill}",
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/a:solidFill/" + colors,
		"p:cxnSp/p:spPr/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill}",
		"p:cxnSp/p:spPr/a:solidFill/" + colors,
	}, Note: "a connector's own fill only: connector paths are never filled, so a style's fill reference shows nothing"},
	{By: "derive.ColorCtx.ReadFill (raw fills)", Dim: Read, Status: PreservedRaw, Paths: []string{
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr,p:cxnSp/p:spPr}/a:blipFill/**",
		"(a:CT_GradientFillProperties)/**", "(a:CT_PatternFillProperties)/**",
	}, Note: "gradient, pattern and picture fills are shown by type, with their XML in --detail full"},
	{By: "derive.ColorCtx.ReadLine (line codec's read half)", Dim: Read, Status: ReadModelled, Paths: []string{
		spPrOwners + "/p:spPr/a:ln", spPrOwners + "/p:spPr/a:ln/@{w,cap}",
		spPrOwners + "/p:spPr/a:ln/{a:noFill,a:solidFill,a:gradFill,a:pattFill}",
		spPrOwners + "/p:spPr/a:ln/a:solidFill/" + colors,
		spPrOwners + "/p:spPr/a:ln/{a:prstDash,a:prstDash/@val,a:custDash}",
		spPrOwners + "/p:spPr/a:ln/{a:headEnd,a:tailEnd}", spPrOwners + "/p:spPr/a:ln/{a:headEnd,a:tailEnd}/@{type,w,len}",
	}},
	{By: "style references (derive.ObjectStyle, StyleEntry)", Dim: Read, Status: OmittedWithReason, Paths: []string{
		"p:sp/p:style/{a:fillRef,a:lnRef}/a:sysClr", "{p:cxnSp,p:pic}/p:style/a:lnRef/a:sysClr",
	}, Note: "a system colour without lastClr as a style's placeholder colour: the hex is omitted"},
	{By: "style references (derive.ObjectStyle, StyleEntry)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:sp/p:style", "p:sp/p:style/{a:fillRef,a:lnRef,a:fontRef}", "p:sp/p:style/{a:fillRef,a:lnRef,a:fontRef}/@idx",
		"p:sp/p:style/{a:fillRef,a:lnRef,a:fontRef}/" + colors,
		"{p:cxnSp,p:pic}/p:style", "{p:cxnSp,p:pic}/p:style/a:lnRef", "{p:cxnSp,p:pic}/p:style/a:lnRef/@idx",
		"{p:cxnSp,p:pic}/p:style/a:lnRef/" + colors,
	}},
	{By: "model.connector (glue)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr", "p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/{a:stCxn,a:endCxn}",
		"p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/{a:stCxn,a:endCxn}/@{id,idx}",
	}},
	{By: "model.picture", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:pic/p:blipFill", "p:pic/p:blipFill/a:blip", "(a:CT_Blip)/@r:embed",
	}},
	{By: "model.picture (derive.ReadPicFill, ReadPicLocks, SVGBlip: crop, fit, fillRect, locks, svg)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:pic/p:blipFill/a:srcRect", "p:pic/p:blipFill/a:srcRect/@{l,t,r,b}",
		"p:pic/p:blipFill/{a:stretch,a:tile}",
		"p:pic/p:blipFill/a:stretch/a:fillRect", "p:pic/p:blipFill/a:stretch/a:fillRect/@{l,t,r,b}",
		"p:pic/p:nvPicPr/p:cNvPicPr", "p:pic/p:nvPicPr/p:cNvPicPr/a:picLocks", "p:pic/p:nvPicPr/p:cNvPicPr/a:picLocks/@*",
		"(a:CT_Blip)/a:extLst", "(a:CT_Blip)/a:extLst/a:ext", "(a:CT_Blip)/a:extLst/a:ext/@uri",
	}, Note: "the SVG extension ({96DAC541-...}, asvg:svgBlip) is shown as svg; other blip extensions are carried unshown. A tiled fill shows as fit tile, its parameters are not shown"},
	{By: "model.text (HTML)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:sp/p:txBody", "p:sp/p:txBody/a:p",
		"p:sp/p:txBody/a:p/a:pPr", "p:sp/p:txBody/a:p/a:pPr/@{algn,lvl}",
		"p:sp/p:txBody/a:p/a:pPr/{a:buNone,a:buChar,a:buChar/@char,a:buAutoNum,a:buAutoNum/@type,a:buBlip}",
		"p:sp/p:txBody/a:p/{a:r,a:fld,a:br}", "p:sp/p:txBody/a:p/{a:r,a:fld}/a:t",
		runProps, runProps + "/@{b,i,u,sz}",
		runProps + "/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill,a:grpFill}",
		runProps + "/a:solidFill/" + colors,
		runProps + "/a:latin", runProps + "/a:latin/@typeface",
	}},
	{By: "model.text (HTML: paragraph spacing, margins, indents, tabs, bullet font, size and colour; run strike, baseline, caps, spacing, highlight, language, hyperlinks)", Dim: Read, Status: ReadModelled, Paths: []string{
		textParaPaths, paraProps + "/{a:lnSpc,a:spcBef,a:spcAft}/{a:spcPct,a:spcPts}", paraProps + "/{a:lnSpc,a:spcBef,a:spcAft}/{a:spcPct,a:spcPts}/@val",
		paraProps + "/a:buClr/" + colors, paraProps + "/{a:buSzPct,a:buSzPts}/@val", paraProps + "/a:buFont/@typeface",
		paraProps + "/a:buAutoNum/@startAt", paraProps + "/a:tabLst/a:tab", paraProps + "/a:tabLst/a:tab/@pos",
		runProps + "/@{strike,baseline,cap,spc}", "p:sp/p:txBody/a:p/a:r/a:rPr/@lang", runProps + "/a:highlight", runProps + "/a:highlight/" + colors,
		runProps + "/a:hlinkClick", runProps + "/a:hlinkClick/@{r:id,action,tooltip}",
	}, Note: "a tab stop shows only with its position; links show their external target (href), else their action or internal target as data-link"},
	{By: "model.text (body properties)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:sp/p:txBody/a:bodyPr", bodyAttrs, autofit, "p:sp/p:txBody/a:bodyPr/a:normAutofit/@{fontScale,lnSpcReduction}",
	}},
	{By: "model.levelProps (inherited text defaults)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:sp/p:txBody/a:lstStyle",
		"p:sldMaster/p:txStyles", "p:sldMaster/p:txStyles/{p:titleStyle,p:bodyStyle,p:otherStyle}",
		"p:presentation/p:defaultTextStyle",
		levels, levels + "/@algn",
		levels + "/{a:buNone,a:buChar,a:buChar/@char,a:buAutoNum,a:buAutoNum/@type,a:buBlip}",
		levels + "/a:defRPr", levels + "/a:defRPr/@{b,i,u,sz}",
		levels + "/a:defRPr/a:solidFill", levels + "/a:defRPr/a:solidFill/" + colors,
		levels + "/a:defRPr/a:latin", levels + "/a:defRPr/a:latin/@typeface",
	}},
	{By: "derive.ParseColor and Scheme.Resolve", Dim: Read, Status: ReadModelled, Paths: []string{
		colorBodies + "/**",
	}, Note: "every modifier is shown and applied (LibreOffice's arithmetic) for the displayed hex"},
	{By: "derive.SlideColorCtx (colour maps)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:sld/p:clrMapOvr", "p:sld/p:clrMapOvr/a:overrideClrMapping", "p:sld/p:clrMapOvr/a:overrideClrMapping/@*",
		"p:sldLayout/p:clrMapOvr", "p:sldLayout/p:clrMapOvr/a:overrideClrMapping", "p:sldLayout/p:clrMapOvr/a:overrideClrMapping/@*",
		"p:sldMaster/p:clrMap", "p:sldMaster/p:clrMap/@*",
	}},
	{By: "theme (derive.ParseScheme, model.loadContext fonts, style matrix)", Dim: Read, Status: ReadModelled, Paths: []string{
		"a:theme/a:themeElements/a:clrScheme", "a:theme/a:themeElements/a:clrScheme/" + slots, "a:theme/a:themeElements/a:clrScheme/" + slots + "/" + colors,
		"a:theme/a:themeElements/a:fontScheme", "a:theme/a:themeElements/a:fontScheme/{a:majorFont,a:minorFont}",
		"a:theme/a:themeElements/a:fontScheme/{a:majorFont,a:minorFont}/{a:latin,a:ea,a:cs}",
		"a:theme/a:themeElements/a:fontScheme/{a:majorFont,a:minorFont}/{a:latin,a:ea,a:cs}/@typeface",
		"a:theme/a:themeElements/a:fmtScheme", "a:theme/a:themeElements/a:fmtScheme/{a:fillStyleLst,a:lnStyleLst}",
		"a:theme/a:themeElements/a:fmtScheme/a:fillStyleLst/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill,a:grpFill}",
		"a:theme/a:themeElements/a:fmtScheme/a:fillStyleLst/a:solidFill/" + colors,
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln", "a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/@{w,cap}",
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/{a:noFill,a:solidFill,a:gradFill,a:pattFill}",
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/a:solidFill/" + colors,
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/{a:prstDash,a:prstDash/@val,a:custDash}",
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/{a:headEnd,a:tailEnd}",
		"a:theme/a:themeElements/a:fmtScheme/a:lnStyleLst/a:ln/{a:headEnd,a:tailEnd}/@{type,w,len}",
	}},
	{By: "theme (raw fills of the style matrix)", Dim: Read, Status: PreservedRaw, Paths: []string{
		"a:theme/a:themeElements/a:fmtScheme/a:fillStyleLst/a:blipFill/**",
	}},
	{By: "model.Deck (slide size), model.ReadSlide (shape tree)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:presentation/p:sldSz", "p:presentation/p:sldSz/@{cx,cy}", "p:sld/p:cSld", "p:sld/p:cSld/p:spTree",
	}},
	{By: "model.object (graphic frame type)", Dim: Read, Status: ReadModelled, Paths: []string{
		"p:graphicFrame/a:graphic", "p:graphicFrame/a:graphic/a:graphicData",
	}},
	{By: "view (raw XML of other objects)", Dim: Read, Status: PreservedRaw, Paths: []string{
		"p:graphicFrame/**", "p:contentPart/**",
	}, Note: "graphic frames and content parts are shown with their exact XML in --detail full"},

	// Design non-goals (docs/design.md, Non-goals and Hard cases).
	{By: "design: effects and 3D are preserved, raw XML only", Dim: Read, Status: NonGoal, Paths: []string{
		"**/{a:effectLst,a:effectDag,a:scene3d,a:sp3d,a:effectRef}", "**/a:effectRef/**",
		"(a:CT_EffectList)/**", "(a:CT_EffectContainer)/**", "(a:CT_Scene3D)/**", "(a:CT_Shape3D)/**",
		"a:theme/a:themeElements/a:fmtScheme/a:effectStyleLst/**",
	}},
	{By: "design: animations and transitions", Dim: Read, Status: NonGoal, Paths: []string{
		"p:sld/{p:transition,p:timing}",
	}},

	// Facets: per-value capabilities of enumerated attributes.
	{By: "derive.PresetSites (the connection-site table)", Facet: "sites", Has: derive.HasSiteTable, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:prstGeom/@prst",
	}, Note: "connection sites are derived for these presets"},
	{By: "derive.ConnectorGeom (tabulated connector paths)", Facet: "endpoints", Has: derive.IsConnectorPreset, Paths: []string{
		"{p:sp,p:cxnSp}/p:spPr/a:prstGeom/@prst",
	}, Note: "from/to endpoints are derived for these presets"},

	{By: "plan compile (internal/pptx/ops/validate.go, text grammar) against edit's generated schema tables", Facet: "validated", Paths: []string{
		"p:sp/p:txBody/a:p/a:pPr/@algn", "p:sp/p:txBody/a:p/a:pPr/a:buAutoNum/@type", "p:sp/p:txBody/a:p/a:r/a:rPr/@u",
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom/@prst",
		spPrOwners + "/p:spPr/a:ln/@cap", spPrOwners + "/p:spPr/a:ln/a:prstDash/@val",
		spPrOwners + "/p:spPr/a:ln/{a:headEnd,a:tailEnd}/@{type,w,len}",
		"{(a:CT_SchemeColor),(a:CT_SystemColor),(a:CT_PresetColor)}/@val",
		"p:sp/p:txBody/a:bodyPr/@{anchor,wrap,vert,vertOverflow,horzOverflow}",
		"p:sp/p:txBody/a:p/a:r/a:rPr/@{strike,cap}", "p:sp/p:txBody/a:p/a:pPr/a:tabLst/a:tab/@algn",
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/@fill",
	}, Note: "a value outside the schema type fails the plan at compile, naming the field, the value and valid ones; the codecs check again before writing"},

	// --- Write: the property codecs (internal/pptx/ops/prop) and the text
	// builder (internal/pptx/ops/text) behind object.set, object.add and
	// text.set. Each path is set by a plan and must read back; written
	// back as shown, it must change nothing. ---
	{By: "prop.Transform (box, rot, flipH, flipV); prop.Endpoints for connectors", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:xfrm", "{p:sp,p:pic,p:cxnSp}/p:spPr/a:xfrm/{a:off,a:ext}",
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:xfrm/{a:off/@x,a:off/@y,a:ext/@cx,a:ext/@cy,@rot,@flipH,@flipV}",
		"p:grpSp/p:grpSpPr/a:xfrm/{a:off/@x,a:off/@y,a:ext/@cx,a:ext/@cy,@rot,@flipH,@flipV}",
	}},
	{By: "prop.Geometry (geom, adj)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom", "{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom/@prst",
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom/a:avLst", "{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom/a:avLst/a:gd",
		"{p:sp,p:pic,p:cxnSp}/p:spPr/a:prstGeom/a:avLst/a:gd/@{name,fmla}",
	}},
	{By: "prop.Path (path on object.set and object.add; a new custGeom gets empty avLst, gdLst, ahLst, cxnLst and the rect l t r b)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:custGeom", "{p:sp,p:pic}/p:spPr/a:custGeom/{a:avLst,a:gdLst,a:ahLst,a:pathLst}",
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path", "{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/@{w,h,stroke}",
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo,a:close}",
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo}/a:pt",
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo}/a:pt/@{x,y}",
	}, Note: "connectors are drawn by their preset: a path is refused"},
	{By: "prop.Path (fill modes)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:pathLst/a:path/@fill",
	}, Values: []string{"none", "lighten", "lightenLess", "darken", "darkenLess"}, Note: "norm, the default, is written as no attribute"},
	{By: "prop.Custom (custom: cxn sites and the text rectangle)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:custGeom/a:cxnLst/**", "{p:sp,p:pic}/p:spPr/a:custGeom/a:rect/**",
	}},
	{By: "prop.Fill, prop.Line, prop.Transform carry arcs; a path write replaces them (design: arcs are outside the path subset)", Dim: Write, Status: PreservedRaw, Paths: []string{
		"p:sp/p:spPr/a:custGeom/a:pathLst/a:path/a:arcTo/**",
	}},
	{By: "design: arcs are excluded from the path subset (DrawingML arcTo has other semantics)", Dim: Write, Status: NonGoal, Paths: []string{
		spPrOwners + "/p:spPr/a:custGeom/a:pathLst/a:path/a:arcTo/**",
	}},
	{By: "prop.Path and prop.Custom carry (formula guides and handles are read-only; extrusionOk is kept)", Dim: Write, Status: PreservedRaw, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:custGeom/**",
	}},
	{By: "prop.Name", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp/p:nvSpPr,p:cxnSp/p:nvCxnSpPr,p:grpSp/p:nvGrpSpPr,p:pic/p:nvPicPr}/p:cNvPr/@name",
	}, Note: "graphic frames are read-only (design: Objects)"},
	{By: "prop.CNvPrText, prop.Hidden, prop.TxBox (descr, title, hidden, txBox)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp/p:nvSpPr,p:cxnSp/p:nvCxnSpPr,p:grpSp/p:nvGrpSpPr,p:pic/p:nvPicPr}/p:cNvPr/@{descr,title,hidden}",
		"p:sp/p:nvSpPr/p:cNvSpPr/@txBox",
	}, Note: "graphic frames are read-only (design: Objects); txBox is a shape's"},
	{By: "prop.Fill (scheme colour names)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"(a:CT_SchemeColor)/@val",
	}, Values: slideSchemeColors(), Note: "phClr, the placeholder colour of theme style matrices, is refused on slides"},
	{By: "prop.Fill", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/{a:noFill,a:solidFill}",
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/a:solidFill/" + colors,
		"{(a:CT_SRgbColor),(a:CT_SystemColor),(a:CT_PresetColor)}/@val",
		"(a:CT_SystemColor)/@lastClr", "(a:CT_HslColor)/@{hue,sat,lum}", "(a:CT_ScRgbColor)/@{r,g,b}",
		colorBodies + "/*", colorBodies + "/*/@val",
	}, Note: "all six colour types, with modifiers; a system colour's lastClr is the displayed hex given without modifiers, or the one it already has"},
	{By: "prop.Fill (connectors: none only)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:cxnSp/p:spPr/a:noFill",
	}, Note: "a connector's fill is never drawn (design: connectors have no fill); the a:noFill PowerPoint and pdraw write is settable"},
	{By: "prop.Line", Dim: Write, Status: RoundTripLaw, Paths: []string{
		spPrOwners + "/p:spPr/a:ln", spPrOwners + "/p:spPr/a:ln/@{w,cap}",
		spPrOwners + "/p:spPr/a:ln/{a:noFill,a:solidFill}",
		spPrOwners + "/p:spPr/a:ln/a:solidFill/" + colors,
		spPrOwners + "/p:spPr/a:ln/{a:prstDash,a:prstDash/@val}",
		spPrOwners + "/p:spPr/a:ln/{a:headEnd,a:tailEnd}", spPrOwners + "/p:spPr/a:ln/{a:headEnd,a:tailEnd}/@{type,w,len}",
	}},
	{By: "prop.Glue (start, end)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/{a:stCxn,a:endCxn}", "p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/{a:stCxn,a:endCxn}/@{id,idx}",
	}},
	{By: "prop.Image (image on object.set and object.add; media part and relationship by ops)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:pic/p:blipFill", "p:pic/p:blipFill/a:blip",
	}},
	{By: "prop.PicFill (crop, fit, fillRect on object.set and object.add)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:pic/p:blipFill/a:srcRect", "p:pic/p:blipFill/a:srcRect/@{l,t,r,b}",
		"p:pic/p:blipFill/a:stretch", "p:pic/p:blipFill/a:stretch/a:fillRect", "p:pic/p:blipFill/a:stretch/a:fillRect/@{l,t,r,b}",
	}, Note: "fit can become stretch; a tile is writable back unchanged only"},
	{By: "prop.Locks (locks on object.set and object.add)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:pic/p:nvPicPr/p:cNvPicPr/a:picLocks", "p:pic/p:nvPicPr/p:cNvPicPr/a:picLocks/@{noGrp,noSelect,noRot,noChangeAspect,noMove,noResize,noEditPoints,noAdjustHandles,noChangeArrowheads,noChangeShapeType,noCrop}",
	}},
	{By: "prop.Body (text: anchor, wrap, insets, autofit, vert, columns and flags on object.set and object.add)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		bodyAttrs, autofit, "p:sp/p:txBody/a:bodyPr/a:normAutofit/@{fontScale,lnSpcReduction}",
	}, Note: "a new a:normAutofit holds only the scales a plan gives: PowerPoint applies the stored scale and does not recompute it on open"},
	{By: "text.Set (text.set, html on object.set and object.add)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		"p:sp/p:txBody", "p:sp/p:txBody/a:bodyPr", "p:sp/p:txBody/a:p",
		"p:sp/p:txBody/a:p/a:pPr", "p:sp/p:txBody/a:p/a:pPr/@{algn,lvl}",
		"p:sp/p:txBody/a:p/a:pPr/{a:buNone,a:buChar,a:buChar/@char,a:buAutoNum,a:buAutoNum/@type}",
		"p:sp/p:txBody/a:p/{a:r,a:br}", "p:sp/p:txBody/a:p/a:r/a:t",
		"p:sp/p:txBody/a:p/a:r/a:rPr", "p:sp/p:txBody/a:p/a:r/a:rPr/@{b,i,u,sz}",
		"p:sp/p:txBody/a:p/a:r/a:rPr/{a:noFill,a:solidFill}",
		"p:sp/p:txBody/a:p/a:r/a:rPr/a:solidFill/" + colors,
		"p:sp/p:txBody/a:p/a:r/a:rPr/a:latin", "p:sp/p:txBody/a:p/a:r/a:rPr/a:latin/@typeface",
	}},
	{By: "text.Set (paragraph spacing, margins, indents, tabs, bullet font, size, colour and start; run strike, baseline, caps, spacing, highlight, language, hyperlinks)", Dim: Write, Status: RoundTripLaw, Paths: []string{
		textParaPaths, paraProps + "/{a:lnSpc,a:spcBef,a:spcAft}/{a:spcPct,a:spcPts}", paraProps + "/{a:lnSpc,a:spcBef,a:spcAft}/{a:spcPct,a:spcPts}/@val",
		paraProps + "/a:buClr/" + colors, paraProps + "/{a:buSzPct,a:buSzPts}/@val", paraProps + "/a:buFont/@typeface",
		paraProps + "/a:buAutoNum/@startAt", paraProps + "/a:tabLst/a:tab", paraProps + "/a:tabLst/a:tab/@{pos,algn}",
		"p:sp/p:txBody/a:p/a:r/a:rPr/@{strike,baseline,cap,spc,lang}", "p:sp/p:txBody/a:p/a:r/a:rPr/a:highlight",
		"p:sp/p:txBody/a:p/a:r/a:rPr/a:highlight/" + colors,
		"p:sp/p:txBody/a:p/a:r/a:rPr/a:hlinkClick", "p:sp/p:txBody/a:p/a:r/a:rPr/a:hlinkClick/@{r:id,tooltip}",
	}, Note: "a hyperlink's relationship is an external hyperlink of the slide, reused when one has the same target, else added with the next free id; a link's relationship is removed when nothing refers to it any more"},

	// Carried: what a codec that rewrites an element keeps of the rest of
	// it, checked by running the rewrite with the path set.
	{By: "text.Set carries (pPr, rPr, fields, breaks, endParaRPr of an edited paragraph)", Dim: Write, Status: PreservedRaw, Paths: []string{
		"p:sp/p:txBody/a:p/**",
	}},
	{By: "prop.Line carries", Dim: Write, Status: PreservedRaw, Paths: []string{spPrOwners + "/p:spPr/a:ln/**"}},
	{By: "prop.Body carries (the rest of a:bodyPr)", Dim: Write, Status: PreservedRaw, Paths: []string{"p:sp/p:txBody/a:bodyPr/**"}},
	{By: "prop.Geometry carries", Dim: Write, Status: PreservedRaw, Paths: []string{"{p:sp,p:pic}/p:spPr/a:prstGeom/**"}},
	{By: "prop.Transform carries", Dim: Write, Status: PreservedRaw, Paths: []string{
		"{p:sp,p:pic}/p:spPr/a:xfrm/**", "p:grpSp/p:grpSpPr/a:xfrm/**",
	}},
	{By: "design: identity", Dim: Write, Status: NonGoal, Paths: []string{nvProps + "/p:cNvPr/@id"},
		Note: "ids are never renumbered (design: Identity); new objects and duplicates get new ones"},
	{By: "prop.Name carries", Dim: Write, Status: PreservedRaw, Paths: []string{
		"{p:sp/p:nvSpPr,p:cxnSp/p:nvCxnSpPr,p:grpSp/p:nvGrpSpPr,p:pic/p:nvPicPr}/p:cNvPr/**",
	}},
	{By: "prop.Glue carries", Dim: Write, Status: PreservedRaw, Paths: []string{"p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/**"}},
	{By: "design: gradient, pattern and picture fills are preserved, raw XML only", Dim: Write, Status: NonGoal, Paths: []string{
		"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/{a:gradFill,a:blipFill,a:pattFill}/**",
		"(a:CT_GradientFillProperties)/**", "(a:CT_PatternFillProperties)/**",
	}},
	{By: "prop.Fill carries (everything in spPr but the fill choice)", Dim: Write, Status: PreservedRaw, Paths: []string{
		"p:sp/p:spPr/**", "p:grpSp/p:grpSpPr/**",
	}, Except: []string{"{p:sp/p:spPr,p:grpSp/p:grpSpPr}/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill,a:grpFill}/**"}},
	{By: "design: effects and 3D are preserved, raw XML only", Dim: Write, Status: NonGoal, Paths: []string{
		"**/{a:effectLst,a:effectDag,a:scene3d,a:sp3d,a:effectRef}", "**/a:effectRef/**",
		"(a:CT_EffectList)/**", "(a:CT_EffectContainer)/**", "(a:CT_Scene3D)/**", "(a:CT_Shape3D)/**",
		"a:theme/a:themeElements/a:fmtScheme/a:effectStyleLst/**",
	}},
	{By: "design: SmartArt, charts, OLE and other graphic frames are read-only", Dim: Write, Status: NonGoal, Paths: []string{
		"p:graphicFrame/**", "p:contentPart/**",
	}},
	{By: "design: animations and transitions", Dim: Write, Status: NonGoal, Paths: []string{
		"p:sld/{p:transition,p:timing}",
	}},
}

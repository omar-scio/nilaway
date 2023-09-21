//	Copyright (c) 2023 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package annotation

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"go.uber.org/nilaway/util"
)

// A ConsumingAnnotationTrigger indicated a possible reason that a nil flow to this site would indicate
// an error
//
// All ConsumingAnnotationTriggers must embed one of the following 4 structs:
// -TriggerIfNonnil
// -TriggerIfDeepNonnil
// -ConsumeTriggerTautology
//
// This is because there are interfaces, such as AdmitsPrimitive, that are implemented only for those
// structs, and to which a ConsumingAnnotationTrigger must be able to be cast
type ConsumingAnnotationTrigger interface {
	// CheckConsume can be called to determined whether this trigger should be triggered
	// given a particular Annotation map
	// for example - an `ArgPass` trigger triggers iff the corresponding function arg has
	// nonNil type
	CheckConsume(Map) bool
	Prestring() Prestring

	// Kind returns the kind of the trigger.
	Kind() TriggerKind

	// UnderlyingSite returns the underlying site this trigger's nilability depends on. If the
	// trigger always or never fires, the site is nil.
	UnderlyingSite() Key

	customPos() (token.Pos, bool)
}

// customPos has the below default implementations, in which case ConsumeTrigger.Pos() will return a default value.
// To return non-default position values, this method should be overridden appropriately.
func (t *TriggerIfNonNil) customPos() (token.Pos, bool)         { return 0, false }
func (t *TriggerIfDeepNonNil) customPos() (token.Pos, bool)     { return 0, false }
func (t *ConsumeTriggerTautology) customPos() (token.Pos, bool) { return 0, false }

// Prestring is an interface used to encode objects that have compact on-the-wire encodings
// (via gob) but can still be expanded into verbose string representations on demand using
// type information. These are key for compact encoding of InferredAnnotationMaps
type Prestring interface {
	String() string
}

// ErrorMessage stores the error message to be displayed when the trigger is fired
type ErrorMessage struct {
	Text string
}

func (e ErrorMessage) String() string {
	return e.Text
}

// TriggerIfNonNil is triggered if the contained Annotation is non-nil
type TriggerIfNonNil struct {
	Ann Key
}

// Kind returns Conditional.
func (t *TriggerIfNonNil) Kind() TriggerKind { return Conditional }

// UnderlyingSite the underlying site this trigger's nilability depends on.
func (t *TriggerIfNonNil) UnderlyingSite() Key { return t.Ann }

// CheckConsume returns true if the underlying annotation is present in the passed map and nonnil
func (t *TriggerIfNonNil) CheckConsume(annMap Map) bool {
	ann, ok := t.Ann.Lookup(annMap)
	return ok && !ann.IsNilable
}

// TriggerIfDeepNonNil is triggered if the contained Annotation is deeply non-nil
type TriggerIfDeepNonNil struct {
	Ann Key
}

// Kind returns DeepConditional.
func (t *TriggerIfDeepNonNil) Kind() TriggerKind { return DeepConditional }

// UnderlyingSite the underlying site this trigger's nilability depends on.
func (t *TriggerIfDeepNonNil) UnderlyingSite() Key { return t.Ann }

// CheckConsume returns true if the underlying annotation is present in the passed map and deeply nonnil
func (t *TriggerIfDeepNonNil) CheckConsume(annMap Map) bool {
	ann, ok := t.Ann.Lookup(annMap)
	return ok && !ann.IsDeepNilable
}

// ConsumeTriggerTautology is used at consumption sites were consuming nil is always an error
type ConsumeTriggerTautology struct{}

// Kind returns Always.
func (*ConsumeTriggerTautology) Kind() TriggerKind { return Always }

// UnderlyingSite always returns nil.
func (*ConsumeTriggerTautology) UnderlyingSite() Key { return nil }

// CheckConsume returns true
func (*ConsumeTriggerTautology) CheckConsume(Map) bool {
	return true
}

// PtrLoad is when a value flows to a point where it is loaded as a pointer
type PtrLoad struct {
	*ConsumeTriggerTautology
}

// Prestring returns this PtrLoad as a Prestring
func (p *PtrLoad) Prestring() Prestring {
	message := "dereferenced"
	return ErrorMessage{Text: message}
}

// MapAccess is when a map value flows to a point where it is indexed, and thus must be non-nil
//
// note: this trigger is produced only if config.ErrorOnNilableMapRead == true
type MapAccess struct {
	*ConsumeTriggerTautology
}

// Prestring returns this MapAccess as a Prestring
func (i *MapAccess) Prestring() Prestring {
	message := "keyed into"
	return ErrorMessage{Text: message}
}

// MapWrittenTo is when a map value flows to a point where one of its indices is written to, and thus
// must be non-nil
type MapWrittenTo struct {
	*ConsumeTriggerTautology
}

// Prestring returns this MapWrittenTo as a Prestring
func (m *MapWrittenTo) Prestring() Prestring {
	message := "written to at an index"
	return ErrorMessage{Text: message}
}

// SliceAccess is when a slice value flows to a point where it is sliced, and thus must be non-nil
type SliceAccess struct {
	*ConsumeTriggerTautology
}

// Prestring returns this SliceAccess as a Prestring
func (s *SliceAccess) Prestring() Prestring {
	message := "sliced into"
	return ErrorMessage{Text: message}
}

// FldAccess is when a value flows to a point where a field of it is accessed, and so it must be non-nil
type FldAccess struct {
	*ConsumeTriggerTautology

	Sel types.Object
}

// Prestring returns this FldAccess as a Prestring
func (f *FldAccess) Prestring() Prestring {
	message := ""

	switch t := f.Sel.(type) {
	case *types.Var:
		message = fmt.Sprintf("accessed field `%s`", t.Name())
	case *types.Func:
		message = fmt.Sprintf("called `%s()`", t.Name())
	default:
		panic(fmt.Sprintf("unexpected Sel type %T in FldAccess", t))
	}

	return ErrorMessage{Text: message}
}

// UseAsErrorResult is when a value flows to the error result of a function, where it is expected to be non-nil
type UseAsErrorResult struct {
	*TriggerIfNonNil

	RetStmt       *ast.ReturnStmt
	IsNamedReturn bool
}

// Prestring returns this UseAsErrorResult as a Prestring
func (u *UseAsErrorResult) Prestring() Prestring {
	retAnn := u.Ann.(*RetAnnotationKey)
	message := ""
	if u.IsNamedReturn {
		message = fmt.Sprintf("returned as named error result `%s` of `%s()`",
			retAnn.FuncDecl.Type().(*types.Signature).Results().At(retAnn.RetNum).Name(), retAnn.FuncDecl.Name())
	} else {
		message = fmt.Sprintf("returned as error result %d of `%s()`", retAnn.RetNum, retAnn.FuncDecl.Name())
	}

	return ErrorMessage{Text: message}
}

// overriding position value to point to the raw return statement, which is the source of the potential error
func (u *UseAsErrorResult) customPos() (token.Pos, bool) {
	if u.IsNamedReturn {
		return u.RetStmt.Pos(), true
	}
	return 0, false
}

// FldAssign is when a value flows to a point where it is assigned into a field
type FldAssign struct {
	*TriggerIfNonNil
}

// Prestring returns this FldAssign as a Prestring
func (f *FldAssign) Prestring() Prestring {
	fldAnn := f.Ann.(*FieldAnnotationKey)
	message := fmt.Sprintf("assigned into field `%s`", fldAnn.FieldDecl.Name())
	return ErrorMessage{Text: message}
}

// ArgFldPass is when a struct field value (A.f) flows to a point where it is passed to a function with a param of
// the same struct type (A)
type ArgFldPass struct {
	*TriggerIfNonNil
	IsPassed bool
}

// Prestring returns this ArgFldPass as a Prestring
func (f *ArgFldPass) Prestring() Prestring {
	ann := f.Ann.(*ParamFieldAnnotationKey)
	recvName := ""
	if ann.IsReceiver() {
		recvName = ann.FuncDecl.Type().(*types.Signature).Recv().Name()
	}

	message := ""
	prefix := ""
	if f.IsPassed {
		prefix = "assigned to "
	}

	if len(recvName) > 0 {
		message = fmt.Sprintf("%sfield `%s` of method receiver `%s`", prefix, ann.FieldDecl.Name(), recvName)
	} else {
		message = fmt.Sprintf("%sfield `%s` of argument %d to `%s()`", prefix, ann.FieldDecl.Name(), ann.ParamNum, ann.FuncDecl.Name())
	}

	return ErrorMessage{Text: message}
}

// GlobalVarAssign is when a value flows to a point where it is assigned into a global variable
type GlobalVarAssign struct {
	*TriggerIfNonNil
}

// Prestring returns this GlobalVarAssign as a Prestring
func (g *GlobalVarAssign) Prestring() Prestring {
	varAnn := g.Ann.(*GlobalVarAnnotationKey)
	message := fmt.Sprintf("assigned into global variable `%s`", varAnn.VarDecl.Name())
	return ErrorMessage{Text: message}
}

// ArgPass is when a value flows to a point where it is passed as an argument to a function. This
// consumer trigger can be used on top of two different sites: ParamAnnotationKey &
// CallSiteParamAnnotationKey. ParamAnnotationKey is the parameter site in the function
// declaration; CallSiteParamAnnotationKey is the argument site in the call expression.
// CallSiteParamAnnotationKey is specifically used for functions with contracts since we need to
// duplicate the sites for context sensitivity.
type ArgPass struct {
	*TriggerIfNonNil
}

// Prestring returns this ArgPass as a Prestring
func (a *ArgPass) Prestring() Prestring {
	message := ""
	switch key := a.Ann.(type) {
	case *ParamAnnotationKey:
		message = fmt.Sprintf("passed as %s to `%s()`", key.MinimalString(), key.FuncDecl.Name())
	case *CallSiteParamAnnotationKey:
		// Location points to the code location of the argument pass at the call site for a ArgPass
		// enclosing CallSiteParamAnnotationKey; Location is empty for a ArgPass enclosing ParamAnnotationKey.
		message = fmt.Sprintf("passed as %s to `%s()` at %s", key.MinimalString(), key.FuncDecl.Name(), key.Location.String())
	default:
		panic(fmt.Sprintf(
			"Expected ParamAnnotationKey or CallSiteParamAnnotationKey but got: %T", key))
	}
	return ErrorMessage{Text: message}
}

// RecvPass is when a receiver value flows to a point where it is used to invoke a method.
// E.g., `s.foo()`, here `s` is a receiver and forms the RecvPass Consumer
type RecvPass struct {
	*TriggerIfNonNil
}

// Prestring returns this RecvPass as a Prestring
func (a *RecvPass) Prestring() Prestring {
	recvAnn := a.Ann.(*RecvAnnotationKey)
	message := fmt.Sprintf("used as receiver to call `%s()`", recvAnn.FuncDecl.Name())
	return ErrorMessage{Text: message}
}

// InterfaceResultFromImplementation is when a result is determined to flow from a concrete method to an interface method via implementation
type InterfaceResultFromImplementation struct {
	*TriggerIfNonNil
	*AffiliationPair
}

// Prestring returns this InterfaceResultFromImplementation as a Prestring
func (i *InterfaceResultFromImplementation) Prestring() Prestring {
	retAnn := i.Ann.(*RetAnnotationKey)
	message := fmt.Sprintf("returned as result %d from interface method `%s()` (implemented by `%s()`)",
		retAnn.RetNum, util.PartiallyQualifiedFuncName(retAnn.FuncDecl), util.PartiallyQualifiedFuncName(i.ImplementingMethod))
	return ErrorMessage{Text: message}
}

// MethodParamFromInterface is when a param flows from an interface method to a concrete method via implementation
type MethodParamFromInterface struct {
	*TriggerIfNonNil
	*AffiliationPair
}

// Prestring returns this MethodParamFromInterface as a Prestring
func (m *MethodParamFromInterface) Prestring() Prestring {
	paramAnn := m.Ann.(*ParamAnnotationKey)
	message := fmt.Sprintf("passed as parameter `%s` to `%s()` (implementing `%s()`)",
		paramAnn.ParamNameString(), util.PartiallyQualifiedFuncName(paramAnn.FuncDecl), util.PartiallyQualifiedFuncName(m.InterfaceMethod))
	return ErrorMessage{Text: message}
}

// DuplicateReturnConsumer duplicates a given consume trigger, assuming the given consumer trigger
// is for a UseAsReturn annotation.
func DuplicateReturnConsumer(t *ConsumeTrigger, location token.Position) *ConsumeTrigger {
	ann := t.Annotation.(*UseAsReturn)
	key := ann.TriggerIfNonNil.Ann.(*RetAnnotationKey)
	return &ConsumeTrigger{
		Annotation: &UseAsReturn{
			TriggerIfNonNil: &TriggerIfNonNil{
				Ann: NewCallSiteRetKey(key.FuncDecl, key.RetNum, location)},
			IsNamedReturn: ann.IsNamedReturn,
			RetStmt:       ann.RetStmt,
		},
		Expr:         t.Expr,
		Guards:       t.Guards.Copy(), // TODO: probably, we might not need a deep copy all the time
		GuardMatched: t.GuardMatched,
	}
}

// UseAsReturn is when a value flows to a point where it is returned from a function.
// This consumer trigger can be used on top of two different sites: RetAnnotationKey &
// CallSiteRetAnnotationKey. RetAnnotationKey is the parameter site in the function declaration;
// CallSiteRetAnnotationKey is the argument site in the call expression. CallSiteRetAnnotationKey is specifically
// used for functions with contracts since we need to duplicate the sites for context sensitivity.
type UseAsReturn struct {
	*TriggerIfNonNil
	IsNamedReturn bool
	RetStmt       *ast.ReturnStmt
}

// Prestring returns this UseAsReturn as a Prestring
func (u *UseAsReturn) Prestring() Prestring {
	message := ""
	switch key := u.Ann.(type) {
	case *RetAnnotationKey:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("returned from `%s()`", key.FuncDecl.Name()))
		if u.IsNamedReturn {
			sb.WriteString(fmt.Sprintf(" via named return `%s`", key.FuncDecl.Type().(*types.Signature).Results().At(key.RetNum).Name()))
		} else {
			sb.WriteString(fmt.Sprintf(" in position %d", key.RetNum))
		}
		message = sb.String()
	case *CallSiteRetAnnotationKey:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("returned from `%s()`", key.FuncDecl.Name()))
		if u.IsNamedReturn {
			sb.WriteString(fmt.Sprintf(" via named return `%s`", key.FuncDecl.Type().(*types.Signature).Results().At(key.RetNum).Name()))
		} else {
			sb.WriteString(fmt.Sprintf(" in position %d", key.RetNum))
		}
		sb.WriteString(fmt.Sprintf(" at %s", key.Location.String()))
		message = sb.String()
	default:
		panic(fmt.Sprintf("Expected RetAnnotationKey or CallSiteRetAnnotationKey but got: %T", key))
	}

	return ErrorMessage{Text: message}
}

// overriding position value to point to the raw return statement, which is the source of the potential error
func (u *UseAsReturn) customPos() (token.Pos, bool) {
	if u.IsNamedReturn {
		return u.RetStmt.Pos(), true
	}
	return 0, false
}

// UseAsFldOfReturn is when a struct field value (A.f) flows to a point where it is returned from a function with the
// return expression of the same struct type (A)
type UseAsFldOfReturn struct {
	*TriggerIfNonNil
}

// Prestring returns this UseAsFldOfReturn as a Prestring
func (u *UseAsFldOfReturn) Prestring() Prestring {
	retAnn := u.Ann.(*RetFieldAnnotationKey)
	message := fmt.Sprintf("field `%s` returned by result %d of `%s()`", retAnn.FieldDecl.Name(), retAnn.RetNum, retAnn.FuncDecl.Name())
	return ErrorMessage{Text: message}
}

// GetRetFldConsumer returns the UseAsFldOfReturn consume trigger with given retKey and expr
func GetRetFldConsumer(retKey Key, expr ast.Expr) *ConsumeTrigger {
	return &ConsumeTrigger{
		Annotation: &UseAsFldOfReturn{
			TriggerIfNonNil: &TriggerIfNonNil{
				Ann: retKey}},
		Expr:   expr,
		Guards: util.NoGuards(),
	}
}

// GetEscapeFldConsumer returns the FldEscape consume trigger with given escKey and selExpr
func GetEscapeFldConsumer(escKey Key, selExpr ast.Expr) *ConsumeTrigger {
	return &ConsumeTrigger{
		Annotation: &FldEscape{
			TriggerIfNonNil: &TriggerIfNonNil{
				Ann: escKey,
			}},
		Expr:   selExpr,
		Guards: util.NoGuards(),
	}
}

// GetParamFldConsumer returns the ArgFldPass consume trigger with given paramKey and expr
func GetParamFldConsumer(paramKey Key, expr ast.Expr) *ConsumeTrigger {
	return &ConsumeTrigger{
		Annotation: &ArgFldPass{
			TriggerIfNonNil: &TriggerIfNonNil{
				Ann: paramKey},
			IsPassed: true,
		},
		Expr:   expr,
		Guards: util.NoGuards(),
	}
}

// SliceAssign is when a value flows to a point where it is assigned into a slice
type SliceAssign struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this SliceAssign as a Prestring
func (f *SliceAssign) Prestring() Prestring {
	fldAnn := f.Ann.(*TypeNameAnnotationKey)
	message := fmt.Sprintf("assigned into a slice of deeply nonnil type `%s`", fldAnn.TypeDecl.Name())
	return ErrorMessage{Text: message}
}

// ArrayAssign is when a value flows to a point where it is assigned into an array
type ArrayAssign struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this ArrayAssign as a Prestring
func (a *ArrayAssign) Prestring() Prestring {
	fldAnn := a.Ann.(*TypeNameAnnotationKey)
	message := fmt.Sprintf("assigned into an array of deeply nonnil type `%s`", fldAnn.TypeDecl.Name())
	return ErrorMessage{Text: message}
}

// PtrAssign is when a value flows to a point where it is assigned into a pointer
type PtrAssign struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this PtrAssign as a Prestring
func (f *PtrAssign) Prestring() Prestring {
	fldAnn := f.Ann.(*TypeNameAnnotationKey)
	message := fmt.Sprintf("assigned into a pointer of deeply nonnil type `%s`", fldAnn.TypeDecl.Name())
	return ErrorMessage{Text: message}
}

// MapAssign is when a value flows to a point where it is assigned into an annotated map
type MapAssign struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this MapAssign as a Prestring
func (f *MapAssign) Prestring() Prestring {
	fldAnn := f.Ann.(*TypeNameAnnotationKey)
	message := fmt.Sprintf("assigned into a map of deeply nonnil type `%s`", fldAnn.TypeDecl.Name())
	return ErrorMessage{Text: message}
}

// DeepAssignPrimitive is when a value flows to a point where it is assigned
// deeply into an unnannotated object
type DeepAssignPrimitive struct {
	*ConsumeTriggerTautology
}

// Prestring returns this Prestring as a Prestring
func (*DeepAssignPrimitive) Prestring() Prestring {
	message := "assigned into a deep type expecting nonnil element type"
	return ErrorMessage{Text: message}
}

// ParamAssignDeep is when a value flows to a point where it is assigned deeply into a function parameter
type ParamAssignDeep struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this ParamAssignDeep as a Prestring
func (p *ParamAssignDeep) Prestring() Prestring {
	message := fmt.Sprintf("assigned deeply into parameter %s", p.Ann.(*ParamAnnotationKey).MinimalString())
	return ErrorMessage{Text: message}
}

// FuncRetAssignDeep is when a value flows to a point where it is assigned deeply into a function return
type FuncRetAssignDeep struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this FuncRetAssignDeep as a Prestring
func (f *FuncRetAssignDeep) Prestring() Prestring {
	retAnn := f.Ann.(*RetAnnotationKey)
	message := fmt.Sprintf("assigned deeply into result %d of `%s()`", retAnn.RetNum, retAnn.FuncDecl.Name())
	return ErrorMessage{Text: message}
}

// VariadicParamAssignDeep is when a value flows to a point where it is assigned deeply into a variadic
// function parameter
type VariadicParamAssignDeep struct {
	*TriggerIfNonNil
}

// Prestring returns this VariadicParamAssignDeep as a Prestring
func (v *VariadicParamAssignDeep) Prestring() Prestring {
	paramAnn := v.Ann.(*ParamAnnotationKey)
	message := fmt.Sprintf("assigned deeply into variadic parameter `%s`", paramAnn.MinimalString())
	return ErrorMessage{Text: message}
}

// FieldAssignDeep is when a value flows to a point where it is assigned deeply into a field
type FieldAssignDeep struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this FieldAssignDeep as a Prestring
func (f *FieldAssignDeep) Prestring() Prestring {
	fldAnn := f.Ann.(*FieldAnnotationKey)
	message := fmt.Sprintf("assigned deeply into field `%s`", fldAnn.FieldDecl.Name())
	return ErrorMessage{Text: message}
}

// GlobalVarAssignDeep is when a value flows to a point where it is assigned deeply into a global variable
type GlobalVarAssignDeep struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this GlobalVarAssignDeep as a Prestring
func (g *GlobalVarAssignDeep) Prestring() Prestring {
	varAnn := g.Ann.(*GlobalVarAnnotationKey)
	message := fmt.Sprintf("assigned deeply into global variable `%s`", varAnn.VarDecl.Name())
	return ErrorMessage{Text: message}
}

// ChanAccess is when a channel is accessed for sending, and thus must be non-nil
type ChanAccess struct {
	*ConsumeTriggerTautology
}

// Prestring returns this MapWrittenTo as a Prestring
func (c *ChanAccess) Prestring() Prestring {
	message := "uninitialized; nil channel accessed"
	return ErrorMessage{Text: message}
}

// LocalVarAssignDeep is when a value flows to a point where it is assigned deeply into a local variable of deeply nonnil type
type LocalVarAssignDeep struct {
	*ConsumeTriggerTautology
	LocalVar *types.Var
}

// Prestring returns this LocalVarAssignDeep as a Prestring
func (l *LocalVarAssignDeep) Prestring() Prestring {
	message := fmt.Sprintf("assigned deeply into local variable `%s`", l.LocalVar.Name())
	return ErrorMessage{Text: message}
}

// ChanSend is when a value flows to a point where it is sent to a channel
type ChanSend struct {
	*TriggerIfDeepNonNil
}

// Prestring returns this ChanSend as a Prestring
func (c *ChanSend) Prestring() Prestring {
	typeAnn := c.Ann.(*TypeNameAnnotationKey)
	message := fmt.Sprintf("sent to channel of deeply nonnil type `%s`", typeAnn.TypeDecl.Name())
	return ErrorMessage{Text: message}
}

// FldEscape is when a nilable value flows through a field of a struct that escapes.
// The consumer is added for the fields at sites of escape.
// There are 2 cases, that we currently consider as escaping:
// 1. If a struct is returned from the function where the field has nilable value,
// e.g, If aptr is pointer in struct A, then  `return &A{}` causes the field aptr to escape
// 2. If a struct is parameter of a function and the field is not initialized
// e.g., if we have fun(&A{}) then the field aptr is considered escaped
// TODO: Add struct assignment as another possible cause of field escape
type FldEscape struct {
	*TriggerIfNonNil
}

// Prestring returns this FldEscape as a Prestring
func (f *FldEscape) Prestring() Prestring {
	ann := f.Ann.(*EscapeFieldAnnotationKey)
	message := fmt.Sprintf("field `%s` escaped out of our analysis scope (presumed nilable)", ann.FieldDecl.Name())
	return ErrorMessage{Text: message}
}

// UseAsNonErrorRetDependentOnErrorRetNilability is when a value flows to a point where it is returned from an error returning function
type UseAsNonErrorRetDependentOnErrorRetNilability struct {
	*TriggerIfNonNil

	IsNamedReturn bool
	RetStmt       *ast.ReturnStmt
}

// Prestring returns this UseAsNonErrorRetDependentOnErrorRetNilability as a Prestring
func (u *UseAsNonErrorRetDependentOnErrorRetNilability) Prestring() Prestring {
	retAnn := u.Ann.(*RetAnnotationKey)

	via := ""
	if u.IsNamedReturn {
		via = fmt.Sprintf(" via named return `%s`", retAnn.FuncDecl.Type().(*types.Signature).Results().At(retAnn.RetNum).Name())
	}
	message := fmt.Sprintf("returned from `%s()`%s in position %d when the error return in position %d is not guaranteed to be non-nil through all paths",
		retAnn.FuncDecl.Name(), via, retAnn.RetNum, retAnn.FuncDecl.Type().(*types.Signature).Results().Len()-1)
	return ErrorMessage{Text: message}
}

// overriding position value to point to the raw return statement, which is the source of the potential error
func (u *UseAsNonErrorRetDependentOnErrorRetNilability) customPos() (token.Pos, bool) {
	if u.IsNamedReturn {
		return u.RetStmt.Pos(), true
	}
	return 0, false
}

// UseAsErrorRetWithNilabilityUnknown is when a value flows to a point where it is returned from an error returning function
type UseAsErrorRetWithNilabilityUnknown struct {
	*TriggerIfNonNil

	IsNamedReturn bool
	RetStmt       *ast.ReturnStmt
}

// Prestring returns this UseAsErrorRetWithNilabilityUnknown as a Prestring
func (u *UseAsErrorRetWithNilabilityUnknown) Prestring() Prestring {
	retAnn := u.Ann.(*RetAnnotationKey)

	message := ""
	if u.IsNamedReturn {
		message = fmt.Sprintf("found in at least one path of `%s()` for the named return `%s` in position %d",
			retAnn.FuncDecl.Name(), retAnn.FuncDecl.Type().(*types.Signature).Results().At(retAnn.RetNum).Name(), retAnn.RetNum)
	} else {
		message = fmt.Sprintf("found in at least one path of `%s()` for the return in position %d", retAnn.FuncDecl.Name(), retAnn.RetNum)
	}
	return ErrorMessage{Text: message}
}

// overriding position value to point to the raw return statement, which is the source of the potential error
func (u *UseAsErrorRetWithNilabilityUnknown) customPos() (token.Pos, bool) {
	if u.IsNamedReturn {
		return u.RetStmt.Pos(), true
	}
	return 0, false
}

// don't modify the ConsumeTrigger and ProduceTrigger objects after construction! Pointers
// to them are duplicated

// A ConsumeTrigger represents a point at which a value is consumed that may be required to be
// non-nil by some Annotation (ConsumingAnnotationTrigger). If Parent is not a RootAssertionNode,
// then that AssertionNode represents the expression that will flow into this consumption point.
// If Parent is a RootAssertionNode, then it will be paired with a ProduceTrigger
//
// Expr should be the expression being consumed, not the expression doing the consumption.
// For example, if the field access x.f requires x to be non-nil, then x should be the
// expression embedded in the ConsumeTrigger not x.f.
//
// The set Guards indicates whether this consumption takes places in a context in which
// it is known to be _guarded_ by one or more conditional checks that refine its behavior.
// This is not _all_ conditional checks this is a very small subset of them.
// Consume triggers become guarded via backpropagation across a check that
// `propagateRichChecks` identified with a `RichCheckEffect`. This pass will
// embed a call to `ConsumeTriggerSliceAsGuarded` that will modify all consume
// triggers for the value targeted by the check as guarded by the guard nonces of the
// flowed `RichCheckEffect`.
//
// Like a nil check, guarding is used to indicate information
// refinement local to one branch. The presence of a guard is overwritten by the absence of a guard
// on a given ConsumeTrigger - see MergeConsumeTriggerSlices. Beyond RichCheckEffects,
// Guards consume triggers can be introduced by other sites that are known to
// obey compatible semantics - such as passing the results of one error-returning function
// directly to a return of another.
//
// ConsumeTriggers arise at consumption sites that may guarded by a meaningful conditional check,
// adding that guard as a unique nonce to the set Guards of the trigger. The guard is added when the
// trigger is propagated across the check, so that when it reaches the statement that relies on the
// guard, the statement can see that the check was performed around the site of the consumption. This
// allows the statement to switch to more permissive semantics.
//
// GuardMatched is a boolean used to indicate that this ConsumeTrigger, by the current point in
// backpropagation, passed through a conditional that granted it a guard, and that that guard was
// determined to match the guard expected by a statement such as `v, ok := m[k]`. Since there could have
// been multiple paths in the CFG between the current point in backpropagation and the site at which the
// trigger arose, GuardMatched is true only if a guard arose and was matched along every path. This
// allows the trigger to maintain its more permissive semantics in later stages of backpropagation.
//
// For some productions, such as reading an index of a map, there is no way for them to generate
// nonnil without such a guarding along every path to their point of consumption, so if GuardMatched
// is not true then they will be replaced (by `checkGuardOnFullTrigger`) with an always-produce-nil
// producer. More explanation of this mechanism is provided in the documentation for
// `RootAssertionNode.AddGuardMatch`
//
// nonnil(Guards)
type ConsumeTrigger struct {
	Annotation   ConsumingAnnotationTrigger
	Expr         ast.Expr
	Guards       util.GuardNonceSet
	GuardMatched bool
}

// Eq compares two ConsumeTrigger pointers for equality
func (c *ConsumeTrigger) Eq(c2 *ConsumeTrigger) bool {
	return reflect.DeepEqual(c.Annotation, c2.Annotation) &&
		c.Expr == c2.Expr &&
		c.Guards.Eq(c2.Guards) &&
		c.GuardMatched == c2.GuardMatched

}

// Pos returns the source position (e.g., line) of the consumer's expression. In special cases, such as named return, it
// returns the position of the stored return AST node
func (c *ConsumeTrigger) Pos() token.Pos {
	if pos, ok := c.Annotation.customPos(); ok {
		return pos
	}
	return c.Expr.Pos()
}

// MergeConsumeTriggerSlices merges two slices of `ConsumeTrigger`s
// its semantics are slightly unexpected only in its treatment of guarding:
// it intersects guard sets
func MergeConsumeTriggerSlices(left, right []*ConsumeTrigger) []*ConsumeTrigger {
	var out []*ConsumeTrigger

	addToOut := func(trigger *ConsumeTrigger) {
		for i, outTrigger := range out {
			if reflect.DeepEqual(outTrigger.Annotation, trigger.Annotation) &&
				outTrigger.Expr == trigger.Expr {
				// intersect guard sets - if a guard isn't present in both branches it can't
				// be considered present before the branch
				out[i] = &ConsumeTrigger{
					Annotation:   outTrigger.Annotation,
					Expr:         outTrigger.Expr,
					Guards:       outTrigger.Guards.Intersection(trigger.Guards),
					GuardMatched: outTrigger.GuardMatched && trigger.GuardMatched,
				}
				return
			}
		}
		out = append(out, trigger)
	}

	for _, l := range left {
		addToOut(l)
	}

	for _, r := range right {
		addToOut(r)
	}

	return out
}

// ConsumeTriggerSliceAsGuarded takes a slice of consume triggers,
// and returns a new slice identical except that each trigger is guarded
func ConsumeTriggerSliceAsGuarded(slice []*ConsumeTrigger, guards ...util.GuardNonce) []*ConsumeTrigger {
	var out []*ConsumeTrigger
	for _, trigger := range slice {
		out = append(out, &ConsumeTrigger{
			Annotation: trigger.Annotation,
			Expr:       trigger.Expr,
			Guards:     trigger.Guards.Copy().Add(guards...),
		})
	}
	return out
}

// ConsumeTriggerSlicesEq returns true if the two passed slices of ConsumeTrigger contain the same elements
// precondition: no duplications
func ConsumeTriggerSlicesEq(left, right []*ConsumeTrigger) bool {
	if len(left) != len(right) {
		return false
	}
lsearch:
	for _, l := range left {
		for _, r := range right {
			if l.Eq(r) {
				continue lsearch
			}
		}
		return false
	}
	return true
}

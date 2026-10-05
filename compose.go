package osmtf

// ObjectMatcher matches an object's tags as a stream. *Matcher implements
// this interface, as do the results of All, Any, and Not.
//
// Call BeginNode, BeginWay, or BeginRelation before each object's tags.
// BeginWay requires the real reference count and closedness, as for Matcher.
// Tag and TagString must not retain or modify their arguments. Matches
// reports the result for the tags received so far without changing state.
// Either result may change as more tags arrive: feed every tag before using
// it as final. The concrete *Matcher has a stronger early-match guarantee.
//
// Pass only non-nil, initialized matchers to the combinators. They retain
// their children, so treat a composition and its children as one mutable
// unit: drive it through the outer matcher and use one composition per
// worker. Reusing a child in multiple positions delivers starts and tags to
// it multiple times. Construction may allocate; the combinators' streaming
// methods add no allocations to those of their children.
type ObjectMatcher interface {
	BeginNode()
	BeginWay(nodeCount int, closed bool)
	BeginRelation()
	Tag(key, value []byte)
	TagString(key, value string)
	Matches() bool
}

// All matches when every child matches the object. Different children may
// match different tags. With no children, All always matches.
// It copies the children slice but retains the child matchers themselves.
func All(children ...ObjectMatcher) ObjectMatcher {
	return &allMatcher{append(matcherGroup(nil), children...)}
}

// Any matches when at least one child matches the object. With no children,
// Any never matches. It copies the children slice but retains the child
// matchers themselves.
func Any(children ...ObjectMatcher) ObjectMatcher {
	return &anyMatcher{append(matcherGroup(nil), children...)}
}

// Not matches when child does not match the object. It complements the whole
// result, including any object-kind restrictions, and retains child. A true
// result can become false after a later tag, so feed every tag before
// accepting it.
func Not(child ObjectMatcher) ObjectMatcher {
	return &notMatcher{child}
}

// matcherGroup forwards every start and tag to every child, even when the
// group's current result would short-circuit its boolean operation.
type matcherGroup []ObjectMatcher

func (g matcherGroup) BeginNode() {
	for _, child := range g {
		child.BeginNode()
	}
}

func (g matcherGroup) BeginWay(nodeCount int, closed bool) {
	for _, child := range g {
		child.BeginWay(nodeCount, closed)
	}
}

func (g matcherGroup) BeginRelation() {
	for _, child := range g {
		child.BeginRelation()
	}
}

func (g matcherGroup) Tag(key, value []byte) {
	for _, child := range g {
		child.Tag(key, value)
	}
}

func (g matcherGroup) TagString(key, value string) {
	for _, child := range g {
		child.TagString(key, value)
	}
}

type allMatcher struct{ matcherGroup }

func (m *allMatcher) Matches() bool {
	for _, child := range m.matcherGroup {
		if !child.Matches() {
			return false
		}
	}
	return true
}

type anyMatcher struct{ matcherGroup }

func (m *anyMatcher) Matches() bool {
	for _, child := range m.matcherGroup {
		if child.Matches() {
			return true
		}
	}
	return false
}

type notMatcher struct{ ObjectMatcher }

func (m *notMatcher) Matches() bool {
	return !m.ObjectMatcher.Matches()
}

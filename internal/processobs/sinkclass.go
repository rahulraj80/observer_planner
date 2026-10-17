package processobs

// classifySinkError resolves the class for THIS observer's sink: the sink's
// own structured classifier first (exact), the shared table second.
func (o *Observer) classifySinkError(err error) SinkErrorClass {
	if err == nil {
		return SinkErrorUnknown
	}
	if c, ok := o.sink.(SinkErrorClassifier); ok {
		if class := c.ClassifySinkError(err); class != SinkErrorUnknown {
			return class
		}
	}
	return ClassifySinkError(err)
}

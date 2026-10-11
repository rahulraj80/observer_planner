// Package vendorsanitize is the one sanitization boundary of the enterprise
// vendor API plan (section 6.5): every typed landing passes it after a
// driver parses a page and before anything lands, logs or renders.
//
// What it does, in order, for every resource and edge of a page:
//
//   - refuses a resource type with no allowlist row (fail closed: a new
//     landing type needs a reviewed row here first);
//   - refuses an identity (external id, parent, edge endpoint) that carries
//     secret material: an id cannot be redacted, so the page fails;
//   - drops every attribute whose name the type's allowlist does not list
//     (counted, never recorded by name);
//   - drops a content-like value in a non-content field: control characters
//     or line breaks, more than vendorapi.MaxAttrValue bytes, or prose (more
//     than maxWords words);
//   - replaces a value carrying a typed secret (the shared scrubber's
//     detector table) with scrub.Redacted.
//
// The Report carries counts only. Pure: no SQL, HTTP, logging or clock
// (imports_test.go pins it).
package vendorsanitize

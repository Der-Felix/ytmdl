package provider

import (
	"context"
	"sort"
	"sync"

	"ytdm/backend/internal/apperr"
)

// Kind names the role a provider fulfils.
type Kind string

const (
	KindMetadata Kind = "metadata"
	KindMedia    Kind = "media"
)

// Info describes a registered provider for the API.
type Info struct {
	Name      string `json:"name"`
	Kind      Kind   `json:"kind"`
	Default   bool   `json:"default"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// Registry holds the configured providers and the defaults used when a request
// does not name one.
type Registry struct {
	mu              sync.RWMutex
	metadata        map[string]MetadataProvider
	media           map[string]MediaProvider
	defaultMetadata string
	defaultMedia    string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		metadata: make(map[string]MetadataProvider),
		media:    make(map[string]MediaProvider),
	}
}

// RegisterMetadata adds a metadata provider.
func (r *Registry) RegisterMetadata(p MetadataProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metadata[p.Name()] = p
	if r.defaultMetadata == "" {
		r.defaultMetadata = p.Name()
	}
}

// RegisterMedia adds a media provider.
func (r *Registry) RegisterMedia(p MediaProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.media[p.Name()] = p
	if r.defaultMedia == "" {
		r.defaultMedia = p.Name()
	}
}

// SetDefaults selects the providers used when a request omits one. Names that
// are not registered are ignored so that a stale configuration cannot break
// the server.
func (r *Registry) SetDefaults(metadata, media string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.metadata[metadata]; ok {
		r.defaultMetadata = metadata
	}
	if _, ok := r.media[media]; ok {
		r.defaultMedia = media
	}
}

// Metadata returns the named metadata provider, or the default when name is
// empty.
func (r *Registry) Metadata(name string) (MetadataProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.defaultMetadata
	}
	if name == "" {
		return nil, apperr.New(apperr.CodeProviderNotFound, "No metadata provider is configured.")
	}
	p, ok := r.metadata[name]
	if !ok {
		return nil, apperr.Newf(apperr.CodeProviderNotFound, "Unknown metadata provider %q.", name)
	}
	return p, nil
}

// Media returns the named media provider, or the default when name is empty.
func (r *Registry) Media(name string) (MediaProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.defaultMedia
	}
	if name == "" {
		return nil, apperr.New(apperr.CodeProviderNotFound, "No media provider is configured.")
	}
	p, ok := r.media[name]
	if !ok {
		return nil, apperr.Newf(apperr.CodeProviderNotFound, "Unknown media provider %q.", name)
	}
	return p, nil
}

// MediaChain returns the media providers to try in order. The preferred
// provider comes first, every other registered provider follows as a fallback.
func (r *Registry) MediaChain(preferred string) []MediaProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if preferred == "" {
		preferred = r.defaultMedia
	}
	out := make([]MediaProvider, 0, len(r.media))
	if p, ok := r.media[preferred]; ok {
		out = append(out, p)
	}
	names := make([]string, 0, len(r.media))
	for name := range r.media {
		if name != preferred {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, r.media[name])
	}
	return out
}

// MetadataChain returns the metadata providers to try in order. The preferred
// provider comes first, every other registered provider follows as a fallback.
func (r *Registry) MetadataChain(preferred string) []MetadataProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if preferred == "" {
		preferred = r.defaultMetadata
	}
	out := make([]MetadataProvider, 0, len(r.metadata))
	if p, ok := r.metadata[preferred]; ok {
		out = append(out, p)
	}
	names := make([]string, 0, len(r.metadata))
	for name := range r.metadata {
		if name != preferred {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, r.metadata[name])
	}
	return out
}

// DefaultMetadataName returns the name of the default metadata provider.
func (r *Registry) DefaultMetadataName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultMetadata
}

// DefaultMediaName returns the name of the default media provider.
func (r *Registry) DefaultMediaName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultMedia
}

// List describes every registered provider, checking availability where the
// provider supports it.
func (r *Registry) List(ctx context.Context) []Info {
	r.mu.RLock()
	metadata := make(map[string]MetadataProvider, len(r.metadata))
	for k, v := range r.metadata {
		metadata[k] = v
	}
	media := make(map[string]MediaProvider, len(r.media))
	for k, v := range r.media {
		media[k] = v
	}
	defMeta, defMedia := r.defaultMetadata, r.defaultMedia
	r.mu.RUnlock()

	infos := make([]Info, 0, len(metadata)+len(media))
	for name, p := range metadata {
		infos = append(infos, describe(ctx, name, KindMetadata, name == defMeta, p))
	}
	for name, p := range media {
		infos = append(infos, describe(ctx, name, KindMedia, name == defMedia, p))
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Kind != infos[j].Kind {
			return infos[i].Kind < infos[j].Kind
		}
		return infos[i].Name < infos[j].Name
	})
	return infos
}

func describe(ctx context.Context, name string, kind Kind, isDefault bool, p any) Info {
	info := Info{Name: name, Kind: kind, Default: isDefault, Available: true}
	if checker, ok := p.(Availability); ok {
		if err := checker.Available(ctx); err != nil {
			info.Available = false
			info.Detail = apperr.MessageOf(err)
		}
	}
	return info
}

# Internal API

This package is created to avoid having extra dependencies in the api package.

Go lang files are symlinked here to avoid coping:

```console
ln -sr ./api/v1alpha2/groupversion_info.go ./internal/api/v1alpha2/groupversion_info.go
ln -sr ./api/v1alpha2/hvpa_types.go ./internal/api/v1alpha2/hvpa_types.go
ln -sr ./api/v1alpha2/zz_generated.deepcopy.go ./internal/api/v1alpha2/zz_generated.deepcopy.go
```

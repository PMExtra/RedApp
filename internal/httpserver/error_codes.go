package httpserver

// Error codes and their HTTP classes, copied from components.x-error-codes in api/openapi.yaml.
// TestErrorCatalogMatchesSpec keeps this table equal to the specification.

type errorCode string

const (
	codeInvalidRequest            errorCode = "INVALID_REQUEST"
	codeValidationFailed          errorCode = "VALIDATION_FAILED"
	codeInvalidQuery              errorCode = "INVALID_QUERY"
	codeInvalidCursor             errorCode = "INVALID_CURSOR"
	codeInvalidPath               errorCode = "INVALID_PATH"
	codeRequestOriginInvalid      errorCode = "REQUEST_ORIGIN_INVALID"
	codeIfMatchRequired           errorCode = "IF_MATCH_REQUIRED"
	codeCurrentPasswordIncorrect  errorCode = "CURRENT_PASSWORD_INCORRECT"
	codePasswordInvalid           errorCode = "PASSWORD_INVALID"
	codeProxyRedactedMismatch     errorCode = "PROXY_REDACTED_MISMATCH"
	codeIconInvalid               errorCode = "ICON_INVALID"
	codePackageInvalid            errorCode = "PACKAGE_INVALID"
	codeInstructionsTrustRequired errorCode = "INSTRUCTIONS_TRUST_REQUIRED"
	codeAuthRequired              errorCode = "AUTH_REQUIRED"
	codeLoginFailed               errorCode = "LOGIN_FAILED"
	codeOriginRejected            errorCode = "ORIGIN_REJECTED"
	codeCSRFRejected              errorCode = "CSRF_REJECTED"
	codeNotFound                  errorCode = "NOT_FOUND"
	codeVendorNotFound            errorCode = "VENDOR_NOT_FOUND"
	codeApplicationNotFound       errorCode = "APPLICATION_NOT_FOUND"
	codeCategoryNotFound          errorCode = "CATEGORY_NOT_FOUND"
	codeFileNotFound              errorCode = "FILE_NOT_FOUND"
	codeSourceNotFound            errorCode = "SOURCE_NOT_FOUND"
	codePreviewNotFound           errorCode = "PREVIEW_NOT_FOUND"
	codeJobNotFound               errorCode = "JOB_NOT_FOUND"
	codeTransferNotFound          errorCode = "TRANSFER_NOT_FOUND"
	codeCacheEntryNotFound        errorCode = "CACHE_ENTRY_NOT_FOUND"
	codeCapabilityUnsupported     errorCode = "CAPABILITY_UNSUPPORTED"
	codeMethodNotAllowed          errorCode = "METHOD_NOT_ALLOWED"
	codeRevisionConflict          errorCode = "REVISION_CONFLICT"
	codeAlreadyExists             errorCode = "ALREADY_EXISTS"
	codeVendorNotEmpty            errorCode = "VENDOR_NOT_EMPTY"
	codeBuiltinProtected          errorCode = "BUILTIN_PROTECTED"
	codeEntityDeleted             errorCode = "ENTITY_DELETED"
	codeApplicationDeletePending  errorCode = "APPLICATION_DELETE_PENDING"
	codeCategoryAmbiguous         errorCode = "CATEGORY_AMBIGUOUS"
	codeFileConflict              errorCode = "FILE_CONFLICT"
	codeTransferIdInUse           errorCode = "TRANSFER_ID_IN_USE"
	codeTransferCancelled         errorCode = "TRANSFER_CANCELLED"
	codePreviewStale              errorCode = "PREVIEW_STALE"
	codeOperationInProgress       errorCode = "OPERATION_IN_PROGRESS"
	codeSourceChanged             errorCode = "SOURCE_CHANGED"
	codeImportNotReady            errorCode = "IMPORT_NOT_READY"
	codePrewarmBusy               errorCode = "PREWARM_BUSY"
	codePrewarmRequestConflict    errorCode = "PREWARM_REQUEST_CONFLICT"
	codeArtifactTooLarge          errorCode = "ARTIFACT_TOO_LARGE"
	codePayloadTooLarge           errorCode = "PAYLOAD_TOO_LARGE"
	codeUnsupportedMediaType      errorCode = "UNSUPPORTED_MEDIA_TYPE"
	codeLoginRateLimited          errorCode = "LOGIN_RATE_LIMITED"
	codePreviewLimitExceeded      errorCode = "PREVIEW_LIMIT_EXCEEDED"
	codeInternalError             errorCode = "INTERNAL_ERROR"
	codeUpstreamUnavailable       errorCode = "UPSTREAM_UNAVAILABLE"
	codeMetadataUntrusted         errorCode = "METADATA_UNTRUSTED"
	codeChannelsUnverified        errorCode = "CHANNELS_UNVERIFIED"
	codeImportSourceFailed        errorCode = "IMPORT_SOURCE_FAILED"
	codeStorageUnavailable        errorCode = "STORAGE_UNAVAILABLE"
	codeNotReady                  errorCode = "NOT_READY"
	codeSessionLimitExceeded      errorCode = "SESSION_LIMIT_EXCEEDED"
	codeTransferCapacity          errorCode = "TRANSFER_CAPACITY"
	codeCacheContended            errorCode = "CACHE_CONTENDED"
	codePreviewBusy               errorCode = "PREVIEW_BUSY"
	codeInstallerUnavailable      errorCode = "INSTALLER_UNAVAILABLE"
	codeCacheMiss                 errorCode = "CACHE_MISS"
)

type errorClass struct {
	status    int
	retryable bool
}

// errorCatalog is the only source of HTTP status and retryability for error responses.
var errorCatalog = map[errorCode]errorClass{
	codeInvalidRequest:            {400, false},
	codeValidationFailed:          {400, false},
	codeInvalidQuery:              {400, false},
	codeInvalidCursor:             {400, false},
	codeInvalidPath:               {400, false},
	codeRequestOriginInvalid:      {400, false},
	codeIfMatchRequired:           {400, false},
	codeCurrentPasswordIncorrect:  {400, false},
	codePasswordInvalid:           {400, false},
	codeProxyRedactedMismatch:     {400, false},
	codeIconInvalid:               {400, false},
	codePackageInvalid:            {400, false},
	codeInstructionsTrustRequired: {400, false},
	codeAuthRequired:              {401, false},
	codeLoginFailed:               {401, false},
	codeOriginRejected:            {403, false},
	codeCSRFRejected:              {403, false},
	codeNotFound:                  {404, false},
	codeVendorNotFound:            {404, false},
	codeApplicationNotFound:       {404, false},
	codeCategoryNotFound:          {404, false},
	codeFileNotFound:              {404, false},
	codeSourceNotFound:            {404, false},
	codePreviewNotFound:           {404, false},
	codeJobNotFound:               {404, false},
	codeTransferNotFound:          {404, false},
	codeCacheEntryNotFound:        {404, false},
	codeCapabilityUnsupported:     {404, false},
	codeMethodNotAllowed:          {405, false},
	codeRevisionConflict:          {409, false},
	codeAlreadyExists:             {409, false},
	codeVendorNotEmpty:            {409, false},
	codeBuiltinProtected:          {409, false},
	codeEntityDeleted:             {409, false},
	codeApplicationDeletePending:  {409, true},
	codeCategoryAmbiguous:         {409, false},
	codeFileConflict:              {409, false},
	codeTransferIdInUse:           {409, false},
	codeTransferCancelled:         {409, false},
	codePreviewStale:              {409, false},
	codeOperationInProgress:       {409, true},
	codeSourceChanged:             {409, true},
	codeImportNotReady:            {409, false},
	codePrewarmBusy:               {409, true},
	codePrewarmRequestConflict:    {409, false},
	codeArtifactTooLarge:          {413, false},
	codePayloadTooLarge:           {413, false},
	codeUnsupportedMediaType:      {415, false},
	codeLoginRateLimited:          {429, true},
	codePreviewLimitExceeded:      {429, true},
	codeInternalError:             {500, false},
	codeUpstreamUnavailable:       {502, true},
	codeMetadataUntrusted:         {502, false},
	codeChannelsUnverified:        {502, true},
	codeImportSourceFailed:        {502, true},
	codeStorageUnavailable:        {503, true},
	codeNotReady:                  {503, true},
	codeSessionLimitExceeded:      {503, true},
	codeTransferCapacity:          {503, true},
	codeCacheContended:            {503, true},
	codePreviewBusy:               {503, true},
	codeInstallerUnavailable:      {503, false},
	codeCacheMiss:                 {504, false},
}

// joinServiceUrl builds a fully-qualified URL by joining the server's
// base URL, a service prefix as returned by /.services, and a route
// path. The prefix is normalised so a bare "/" mount (root-named
// service) and a trailing-slash mount (e.g. `--mount=/foo/bar/`) both
// produce a single-slash join with the route path.
export const joinServiceUrl = (baseUrl, prefix, path) => {
    let p = prefix ?? '';
    if (p === '' || p === '/') return `${baseUrl}${path}`;
    return `${baseUrl}${p.replace(/\/+$/, '')}${path}`;
};

// resolveConfigUrl resolves one of the appConfig.*Url fields sent by the
// index.html template, which already joins AppConfig.BaseURL onto the path
// server-side. BaseURL is empty by default (same-origin), so the field is a
// bare path and origin must be added; but it can be a customer's public URL
// (APP_BASE_URL, for redirect links a payment engine builds), in which case
// the field already arrives absolute and must be used as-is.
export const resolveConfigUrl = (origin, v) => {
    if (!v) return '';
    return /^https?:\/\//i.test(v) ? v : `${origin}${v}`;
};

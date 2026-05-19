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

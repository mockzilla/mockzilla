export const isJSONContentType = contentType => {
    const mediaType = (contentType || '').split(';')[0].trim().toLowerCase();

    return mediaType === 'application/json' || mediaType.endsWith('+json');
}

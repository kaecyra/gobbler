// A local stand-in for YouTube's image host (i.ytimg.com), so no e2e server
// downloads a thumbnail from the real internet. A server under test is pointed
// at it with GOBBLER_VIDEO_THUMBNAIL_HOST (see THUMBNAIL_HOST_ENV). It answers
// /vi/<id>/hqdefault.jpg for any 11-character video ID with a small JPEG and
// everything else with 404, the shape the real host has. Loopback only.
import { createServer } from 'node:http';

export const THUMBNAIL_HOST_ENV = 'GOBBLER_VIDEO_THUMBNAIL_HOST';

// A 2x2 red JPEG.
export const THUMBNAIL_JPEG = Buffer.from(
  '/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIAAIAAgMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AOQooorxT9MP/9k=',
  'base64',
);

// startThumbnailHost listens on an OS-chosen loopback port, so parallel suites
// never collide. It resolves to the origin and a close function.
export async function startThumbnailHost() {
  const server = createServer((req, res) => {
    if (req.method === 'GET' && /^\/vi\/[A-Za-z0-9_-]{11}\/hqdefault\.jpg$/.test(req.url ?? '')) {
      res.writeHead(200, { 'Content-Type': 'image/jpeg', 'Content-Length': THUMBNAIL_JPEG.length });
      res.end(THUMBNAIL_JPEG);
      return;
    }
    res.writeHead(404).end();
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const { port } = server.address();
  return {
    origin: `http://127.0.0.1:${port}`,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

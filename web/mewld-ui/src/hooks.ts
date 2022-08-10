import cookie from 'cookie';
import type { Handle } from '@sveltejs/kit';
import type { GetSession } from '@sveltejs/kit';
import * as logger from './lib/logger';

export const handle: Handle = async ({ event, resolve }) => {
  const cookies = cookie.parse(event.request.headers.get('cookie') || '');

  const response = await resolve(event);

  return response;
};

export const getSession: GetSession = async (event) => {
  const cookies = cookie.parse(
    event.request.headers.get('cookie') || event.request.headers.get('Cookie') || ''
  );

  let id = ''
  let instanceUrl = ''

  if (cookies['session']) {
    id = cookies['session'];
    instanceUrl = cookies["instanceUrl"]
  }

  if(!instanceUrl) {
    id = ""
  }

  if(id) {
    let pingCheck = await fetch(`${instanceUrl}/ping`, {
      headers: {
        'X-Session': id
      }
    })

    let pingText = await pingCheck.text()

    if(pingText != "pong") {
      id = ""
    }
  }

  return {id: id, instanceUrl: instanceUrl};
};

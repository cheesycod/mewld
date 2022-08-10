<script context="module">
    /** @type {import('@sveltejs/kit').Load} */
    export async function load({ fetch, session }) {
        if(!session.id) {
            return {
                status: 401,
                error: new Error("Not logged in")
            }
        }

        let res = await fetch(`${session.instanceUrl}/action-logs`, {
            headers: {
                "X-Session": session.id
            }
        });
    
        if (!res.ok) {
            return {
                status: res.status,
                error: new Error("Could not load clusters:" + await res.text()),
            };
        }

        let html = ``;

        let i = 0

        let data = await res.json()

        data.forEach(el => {
            console.log(el)

            html += "<div>"

            let ald = ""

            switch (el.event) {
                case "shards_launched":
                    if(!el.cluster && el.cluster != 0) {
                        el.cluster = '<span class="unknown">unknown</span>'
                    }
                    html += `<p class="clickable" onclick="showAld(${i})" id="alp-${i}">Cluster ${el.cluster} launched successfully</p>`;
                    ald += `<strong>Cluster:</strong> ${el.cluster}<br/><strong>From shard:</strong> ${el.from}<br/><strong>To shard:</strong> ${el.to}`;
                    break;
                case "rolling_restart":
                    html += `<p class="clickable" onclick="showAld(${i})" id="alp-${i}">Rolling restart begun (instance wide)</p>`;
                    break;
                default:
                    html += `<p class="clickable" onclick="showAld(${i})" id="alp-${i}">Unknown event: ${JSON.stringify(el)}</p>`;
                    break;
            }

            html += `
                <div class="ald" style="display: none" id="ald-${i}">
                    <strong>Timestamp: </strong><span class="ts">${new Date(el.ts/1000)}</span><br/>
                    ${ald}
                </div>
            </div>`

            i++
        })

        return {
            props: {
                html: html
            }
        }
    }
  
  </script>
  <script lang="ts">
import { browser } from "$app/env";


    export let html;

    function showAld(i) {
        let el = (document.querySelector(`#ald-${i}`) as HTMLInputElement)

        if(el.style.display == "none") {
            el.style.display = "initial"
        } else {
            el.style.display = "none"
        }
    }

    if(browser) {
        window.showAld = showAld
    }
  </script>


{@html html}
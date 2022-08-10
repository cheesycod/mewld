<script context="module">
  /** @type {import('@sveltejs/kit').Load} */
  export async function load({ fetch, session }) {
    let res = await fetch(`${session.instanceUrl}/instance-list`, {
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

    return {
        props: {
            instances: await res.json()
        }
    }
  }

</script>

<script lang="ts">
    export let instances: any;

    async function renderClusterExt(cid) {

    }
</script>

{JSON.stringify(instances)}

<div id="cluster-list">
  {#each instances.Map as cluster, i}
    <div class="cluster" on:click={() => renderClusterExt(cluster.ID)}>
      <p class="cluster-para">{cluster.ID}. {cluster.Name}</p>
      <div class="cluster-pane clickable" id="c-{cluster.ID}">
        <strong>Session ID:</strong> {instances.Instances[i].SessionID}<br/>
        <strong>Shards:</strong> {instances.Instances[i].Shards.join(', ')}<br/>
        <strong>Started At:</strong> {instances.Instances[i].StartedAt}<br/>
        <strong>Active:</strong> {instances.Instances[i].Active}<br/>
        
        <div id="c-{instances.Instances[i].ClusterID}-health" style="margin-bottom: 10px">
            <span style="font-weight: bold">Click here to manage this cluster and fetch health information about it</span>
        </div>    
      </div>
    </div>
  {/each}
</div>
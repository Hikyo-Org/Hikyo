"""Closed single-job semantics needed to infer an unstarted sandbox job."""
import yaml


class ClosedLoader(yaml.BaseLoader):
    def construct_mapping(self, node, deep=False):
        result = {}
        for key_node, value_node in node.value:
            key = self.construct_object(key_node, deep=deep)
            if type(key) is not str or key in result:
                raise ValueError('release-workflow:duplicate-or-nonscalar-key')
            result[key] = self.construct_object(value_node, deep=deep)
        return result


def single_sandbox_checked(m, body):
    m.require(type(body) is str and 0 < len(body.encode()) <= 262144, 'bounded-workflow-body')
    try:
        m.require(not any(isinstance(token, (yaml.AliasToken, yaml.AnchorToken))
                          for token in yaml.scan(body)), 'no-workflow-aliases-or-anchors')
        value = yaml.load(body, Loader=ClosedLoader)
    except yaml.YAMLError:
        raise ValueError('release-workflow:invalid-yaml') from None
    m.canonical(value)
    m.require(type(value) is dict and type(value.get('on')) is dict
              and set(value['on']) == {'workflow_dispatch'}
              and type(value.get('jobs')) is dict and set(value['jobs']) == {'sandbox'},
              'single-manual-sandbox-workflow')
    job = value['jobs']['sandbox']
    m.require(type(job) is dict and not {'strategy', 'needs', 'uses'} & set(job)
              and job.get('name', 'sandbox') == 'sandbox'
              and job.get('environment') == 'hikyo-158-aws-sandbox'
              and job.get('runs-on') == 'ubuntu-latest'
              and job.get('if') == "github.repository == 'Hikyo-Org/Hikyo' && github.ref == 'refs/heads/main'"
              and job.get('permissions') == {'contents': 'read', 'id-token': 'write'}
              and type(job.get('steps')) is list and job['steps'],
              'fixed-unstarted-sandbox-topology-and-authority')
    return value
